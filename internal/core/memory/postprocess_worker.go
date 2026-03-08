package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

type PostprocessWorkerOptions struct {
	Enabled      bool
	PollInterval time.Duration
	BatchSize    int
	WorkerCount  int
	Lease        time.Duration
	MaxAttempts  int
	RetryBase    time.Duration
	RetryMax     time.Duration
}

func DefaultPostprocessWorkerOptions() PostprocessWorkerOptions {
	return PostprocessWorkerOptions{
		Enabled:      true,
		PollInterval: 250 * time.Millisecond,
		BatchSize:    32,
		WorkerCount:  2,
		Lease:        30 * time.Second,
		MaxAttempts:  5,
		RetryBase:    500 * time.Millisecond,
		RetryMax:     60 * time.Second,
	}
}

func normalizePostprocessWorkerOptions(in PostprocessWorkerOptions) PostprocessWorkerOptions {
	out := in
	defaults := DefaultPostprocessWorkerOptions()
	if out.PollInterval <= 0 {
		out.PollInterval = defaults.PollInterval
	}
	if out.BatchSize <= 0 {
		out.BatchSize = defaults.BatchSize
	}
	if out.WorkerCount <= 0 {
		out.WorkerCount = defaults.WorkerCount
	}
	if out.Lease <= 0 {
		out.Lease = defaults.Lease
	}
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaults.MaxAttempts
	}
	if out.RetryBase <= 0 {
		out.RetryBase = defaults.RetryBase
	}
	if out.RetryMax <= 0 {
		out.RetryMax = defaults.RetryMax
	}
	if out.RetryBase > out.RetryMax {
		out.RetryBase = out.RetryMax
	}
	return out
}

func (s *Service) StartPostprocessWorkers(
	parent context.Context,
	opts PostprocessWorkerOptions,
) (func(), error) {
	if parent == nil {
		parent = context.Background()
	}
	opts = normalizePostprocessWorkerOptions(opts)
	if !opts.Enabled {
		return func() {}, nil
	}
	jobRepo, ok := s.repo.(domain.MemoryPostprocessJobRepository)
	if !ok || jobRepo == nil {
		return nil, fmt.Errorf("memory repository does not support postprocess jobs")
	}

	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	for i := 0; i < opts.WorkerCount; i++ {
		owner := fmt.Sprintf("postprocess-%d-%d", time.Now().UnixNano(), i+1)
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			s.runPostprocessWorkerLoop(ctx, jobRepo, owner, opts)
		}(owner)
	}

	stop := func() {
		cancel()
		wg.Wait()
	}
	return stop, nil
}

func (s *Service) runPostprocessWorkerLoop(
	ctx context.Context,
	jobRepo domain.MemoryPostprocessJobRepository,
	owner string,
	opts PostprocessWorkerOptions,
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		now := time.Now().UTC()
		jobs, err := jobRepo.ClaimPostprocessJobs(ctx, domain.MemoryPostprocessClaimOptions{
			Owner:      owner,
			Limit:      opts.BatchSize,
			Now:        now,
			LeaseUntil: now.Add(opts.Lease),
		})
		if err != nil {
			s.logDebugf("[pali-postprocess] worker=%s claim_error=%v", owner, err)
			if !sleepUntilNextPoll(ctx, opts.PollInterval) {
				return
			}
			continue
		}
		if len(jobs) == 0 {
			if !sleepUntilNextPoll(ctx, opts.PollInterval) {
				return
			}
			continue
		}

		for _, job := range jobs {
			if ctx.Err() != nil {
				return
			}
			s.processPostprocessJob(ctx, jobRepo, job, opts)
		}
	}
}

func (s *Service) processPostprocessJob(
	ctx context.Context,
	jobRepo domain.MemoryPostprocessJobRepository,
	job domain.MemoryPostprocessJob,
	opts PostprocessWorkerOptions,
) {
	err := s.executePostprocessJob(ctx, jobRepo, job, opts)
	now := time.Now().UTC()
	if err == nil {
		if markErr := jobRepo.MarkPostprocessJobSucceeded(ctx, job.ID, now); markErr != nil {
			s.logDebugf("[pali-postprocess] job=%s mark_succeeded_error=%v", job.ID, markErr)
		}
		return
	}

	attempts := job.Attempts + 1
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = opts.MaxAttempts
	}
	status := domain.PostprocessJobStatusFailed
	nextAvailable := now.Add(retryDelay(attempts, opts.RetryBase, opts.RetryMax))
	if attempts >= maxAttempts {
		status = domain.PostprocessJobStatusDeadLetter
		nextAvailable = now
	}
	if markErr := jobRepo.MarkPostprocessJobFailed(
		ctx,
		job.ID,
		now,
		nextAvailable,
		attempts,
		status,
		strings.TrimSpace(err.Error()),
	); markErr != nil {
		s.logDebugf("[pali-postprocess] job=%s mark_failed_error=%v", job.ID, markErr)
	}
}

func (s *Service) executePostprocessJob(
	ctx context.Context,
	jobRepo domain.MemoryPostprocessJobRepository,
	job domain.MemoryPostprocessJob,
	opts PostprocessWorkerOptions,
) error {
	switch job.JobType {
	case domain.PostprocessJobTypeVectorUpsert:
		return s.handleVectorUpsertPostprocessJob(ctx, job)
	case domain.PostprocessJobTypeParserExtract:
		return s.handleParserExtractPostprocessJob(ctx, jobRepo, job, opts)
	default:
		return fmt.Errorf("unsupported postprocess job type %q", job.JobType)
	}
}

func (s *Service) handleVectorUpsertPostprocessJob(
	ctx context.Context,
	job domain.MemoryPostprocessJob,
) error {
	memories, err := s.repo.GetByIDs(ctx, job.TenantID, []string{job.MemoryID})
	if err != nil {
		return err
	}
	if len(memories) == 0 {
		// idempotent no-op for already-deleted rows
		return nil
	}
	memory := memories[0]
	embedding, err := s.embedder.Embed(ctx, embeddingLookupTextForMemory(memory))
	if err != nil {
		s.markIndexState(
			ctx,
			job.TenantID,
			[]string{job.MemoryID},
			domain.MemoryIndexOperationUpsert,
			domain.MemoryIndexStateFailed,
			err,
		)
		return err
	}
	s.markIndexState(
		ctx,
		job.TenantID,
		[]string{job.MemoryID},
		domain.MemoryIndexOperationUpsert,
		domain.MemoryIndexStatePending,
		nil,
	)
	if err := s.vector.Upsert(ctx, job.TenantID, job.MemoryID, embedding); err != nil {
		s.markIndexState(
			ctx,
			job.TenantID,
			[]string{job.MemoryID},
			domain.MemoryIndexOperationUpsert,
			domain.MemoryIndexStateFailed,
			err,
		)
		return err
	}
	s.markIndexState(
		ctx,
		job.TenantID,
		[]string{job.MemoryID},
		domain.MemoryIndexOperationUpsert,
		domain.MemoryIndexStateIndexed,
		nil,
	)
	return nil
}

func (s *Service) handleParserExtractPostprocessJob(
	ctx context.Context,
	jobRepo domain.MemoryPostprocessJobRepository,
	job domain.MemoryPostprocessJob,
	opts PostprocessWorkerOptions,
) error {
	if !s.parser.Enabled || s.infoParser == nil {
		return nil
	}
	memories, err := s.repo.GetByIDs(ctx, job.TenantID, []string{job.MemoryID})
	if err != nil {
		return err
	}
	if len(memories) == 0 {
		// idempotent no-op for already-deleted rows
		return nil
	}
	raw := memories[0]
	if raw.Kind != domain.MemoryKindRawTurn {
		return nil
	}

	parsed, err := s.parseFactsWithFallback(ctx, raw.Content, 1)
	if err != nil {
		return err
	}

	entityFacts := make([]domain.EntityFact, 0, len(parsed.Facts))
	vectorJobs := make([]domain.MemoryPostprocessJobEnqueue, 0, len(parsed.Facts))
	seenVectorMemory := make(map[string]struct{}, len(parsed.Facts))

	for factIdx, fact := range parsed.Facts {
		memory, created, err := s.applyParsedFactMetadataOnly(
			ctx,
			raw.TenantID,
			raw.Content,
			raw.Tags,
			raw.Source,
			fact,
			factIdx,
			parsed.Extractor,
			parsed.ExtractorVersion,
		)
		if err != nil {
			return err
		}
		if memory == nil {
			continue
		}
		if created {
			if _, ok := seenVectorMemory[memory.ID]; !ok {
				seenVectorMemory[memory.ID] = struct{}{}
				vectorJobs = append(vectorJobs, domain.MemoryPostprocessJobEnqueue{
					IngestID:    job.IngestID,
					TenantID:    memory.TenantID,
					MemoryID:    memory.ID,
					JobType:     domain.PostprocessJobTypeVectorUpsert,
					MaxAttempts: opts.MaxAttempts,
				})
			}
		}
		if entityFact, ok := buildEntityFactRecord(*memory, fact); ok {
			entityFacts = append(entityFacts, entityFact)
		}
	}

	if err := s.storeEntityFacts(ctx, entityFacts); err != nil {
		return err
	}
	if len(vectorJobs) > 0 {
		if _, err := jobRepo.EnqueuePostprocessJobs(ctx, vectorJobs, opts.MaxAttempts); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyParsedFactMetadataOnly(
	ctx context.Context,
	tenantID string,
	sourceContent string,
	baseTags []string,
	baseSource string,
	fact ParsedFact,
	factIndex int,
	extractor string,
	extractorVersion string,
) (*domain.Memory, bool, error) {
	content := normalizeFactContent(fact.Content)
	if !shouldStoreParsedFactContent(content) {
		return nil, false, nil
	}
	kind := resolveKind(fact.Kind)
	if kind != domain.MemoryKindEvent && kind != domain.MemoryKindObservation {
		kind = domain.MemoryKindObservation
	}
	identity := buildParsedFactIdentity(sourceContent, factIndex, fact, extractor, extractorVersion)

	exactMatch, err := s.findMemoryByCanonicalKey(ctx, tenantID, identity.CanonicalKey)
	if err != nil {
		return nil, false, err
	}
	if exactMatch != nil {
		return exactMatch, false, nil
	}
	exactMatch, err = s.findMemoryByRelationTuple(ctx, tenantID, fact)
	if err != nil {
		return nil, false, err
	}
	if exactMatch != nil {
		return exactMatch, false, nil
	}

	importance := 0.0
	if s.scorer != nil {
		importance, err = s.scorer.Score(ctx, content)
		if err != nil {
			return nil, false, err
		}
	}

	stored, err := s.storeInRepo(ctx, []domain.Memory{
		applyIdentityToMemory(domain.Memory{
			TenantID:      tenantID,
			Content:       content,
			QueryViewText: fact.QueryViewText,
			Tier:          domain.MemoryTierSemantic,
			Kind:          kind,
			Tags:          mergeTags(baseTags, append(append([]string{}, fact.Tags...), "memory_op:add", "memory_state:active")...),
			Source:        appendDerivedSource(baseSource, "parser"),
			CreatedBy:     domain.MemoryCreatedBySystem,
			Importance:    importance,
		}, identity),
	})
	if err != nil {
		return nil, false, err
	}
	if len(stored) != 1 {
		return nil, false, fmt.Errorf("parsed fact store returned %d records", len(stored))
	}
	return &stored[0], true, nil
}

func retryDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt <= 1 {
		return base
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= max {
			return max
		}
		if delay > max/2 {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

func sleepUntilNextPoll(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
