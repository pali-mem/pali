package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// IngestAsync stores a single memory and queues async work.
func (s *Service) IngestAsync(
	ctx context.Context,
	in StoreInput,
	maxAttempts int,
) (domain.MemoryIngestReceipt, error) {
	return s.IngestBatchAsync(ctx, []StoreInput{in}, maxAttempts)
}

// IngestBatchAsync stores a batch of memories and queues async work.
func (s *Service) IngestBatchAsync(
	ctx context.Context,
	inputs []StoreInput,
	maxAttempts int,
) (domain.MemoryIngestReceipt, error) {
	if len(inputs) == 0 {
		return domain.MemoryIngestReceipt{}, domain.ErrInvalidInput
	}
	repo, ok := s.repo.(domain.MemoryAsyncIngestRepository)
	if !ok || repo == nil {
		return domain.MemoryIngestReceipt{}, fmt.Errorf("memory repository does not support async ingest")
	}
	prepared, err := s.prepareStoreInputs(ctx, inputs)
	if err != nil {
		return domain.MemoryIngestReceipt{}, err
	}

	items := make([]domain.MemoryAsyncIngestItem, 0, len(prepared))
	for _, item := range prepared {
		memory := applyImplicitMemoryIdentity(domain.Memory{
			TenantID:  item.input.TenantID,
			Content:   item.input.Content,
			Tier:      item.resolvedTier,
			Kind:      item.resolvedKind,
			Tags:      append([]string{}, item.input.Tags...),
			Source:    item.input.Source,
			CreatedBy: item.input.CreatedBy,
		})
		items = append(items, domain.MemoryAsyncIngestItem{
			Memory:      memory,
			QueueVector: true,
			QueueParser: s.parser.Enabled && item.resolvedKind == domain.MemoryKindRawTurn && s.infoParser != nil,
		})
	}
	return repo.StoreBatchAsyncIngest(ctx, items, maxAttempts)
}

// GetPostprocessJob returns a postprocess job by ID.
func (s *Service) GetPostprocessJob(ctx context.Context, jobID string) (*domain.MemoryPostprocessJob, error) {
	repo, ok := s.repo.(domain.MemoryPostprocessJobRepository)
	if !ok || repo == nil {
		return nil, fmt.Errorf("memory repository does not support postprocess job queries")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, domain.ErrInvalidInput
	}
	job, err := repo.GetPostprocessJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, domain.ErrNotFound
	}
	return job, nil
}

// ListPostprocessJobs lists postprocess jobs for a tenant.
func (s *Service) ListPostprocessJobs(
	ctx context.Context,
	filter domain.MemoryPostprocessJobFilter,
) ([]domain.MemoryPostprocessJob, error) {
	repo, ok := s.repo.(domain.MemoryPostprocessJobRepository)
	if !ok || repo == nil {
		return nil, fmt.Errorf("memory repository does not support postprocess job queries")
	}
	if tenantID := strings.TrimSpace(filter.TenantID); tenantID != "" {
		if err := s.ensureTenantExists(ctx, tenantID); err != nil {
			return nil, err
		}
		filter.TenantID = tenantID
	}
	return repo.ListPostprocessJobs(ctx, filter)
}
