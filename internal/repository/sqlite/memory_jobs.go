package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

// MarkIndexState updates memory index state rows.
func (r *MemoryRepository) MarkIndexState(
	ctx context.Context,
	tenantID string,
	memoryIDs []string,
	op domain.MemoryIndexOperation,
	state domain.MemoryIndexState,
	lastError string,
) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(string(op)) == "" || strings.TrimSpace(string(state)) == "" {
		return domain.ErrInvalidInput
	}
	unique := uniqueNonEmptyStrings(memoryIDs)
	if len(unique) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastError = strings.TrimSpace(lastError)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index-state transaction: %w", err)
	}
	defer rollbackTx(tx)

	for _, memoryID := range unique {
		result, err := tx.ExecContext(
			ctx,
			UpdateMemoryIndexJobStateSQL,
			string(state),
			lastError,
			string(state),
			now,
			tenantID,
			memoryID,
			string(op),
		)
		if err != nil {
			return fmt.Errorf("update index job state: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("index job rows affected: %w", err)
		}
		if affected == 0 {
			if err := upsertMemoryIndexJobTx(
				ctx,
				tx,
				tenantID,
				memoryID,
				op,
				state,
				lastError,
			); err != nil {
				return fmt.Errorf("upsert missing index job: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit index-state transaction: %w", err)
	}
	return nil
}

// StoreBatchAsyncIngest stores memories and enqueues async jobs atomically.
func (r *MemoryRepository) StoreBatchAsyncIngest(
	ctx context.Context,
	items []domain.MemoryAsyncIngestItem,
	maxAttempts int,
) (domain.MemoryIngestReceipt, error) {
	if len(items) == 0 {
		return domain.MemoryIngestReceipt{}, domain.ErrInvalidInput
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	ingestID := newID("ing")
	now := time.Now().UTC()
	nowRaw := now.Format(time.RFC3339Nano)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MemoryIngestReceipt{}, fmt.Errorf("begin async-ingest transaction: %w", err)
	}
	defer rollbackTx(tx)

	memoryIDs := make([]string, 0, len(items))
	jobIDs := make([]string, 0, len(items)*2)
	seenJobs := make(map[string]struct{}, len(items)*2)

	for i := range items {
		item := items[i]
		memory := item.Memory
		if err := prepareMemoryForStore(&memory, now); err != nil {
			return domain.MemoryIngestReceipt{}, fmt.Errorf("prepare ingest memory[%d]: %w", i, err)
		}

		tagsJSON, err := json.Marshal(memory.Tags)
		if err != nil {
			return domain.MemoryIngestReceipt{}, fmt.Errorf("marshal ingest tags[%d]: %w", i, err)
		}
		if err := insertMemoryTx(ctx, tx, memory, string(tagsJSON)); err != nil {
			return domain.MemoryIngestReceipt{}, fmt.Errorf("insert ingest memory[%d]: %w", i, err)
		}
		memoryIDs = append(memoryIDs, memory.ID)

		if item.QueueVector {
			jobID, err := upsertMemoryPostprocessJobTx(
				ctx,
				tx,
				domain.MemoryPostprocessJobEnqueue{
					IngestID:    ingestID,
					TenantID:    memory.TenantID,
					MemoryID:    memory.ID,
					JobType:     domain.PostprocessJobTypeVectorUpsert,
					MaxAttempts: maxAttempts,
				},
				nowRaw,
			)
			if err != nil {
				return domain.MemoryIngestReceipt{}, fmt.Errorf("queue vector job[%d]: %w", i, err)
			}
			if _, ok := seenJobs[jobID]; !ok {
				seenJobs[jobID] = struct{}{}
				jobIDs = append(jobIDs, jobID)
			}
		}
		if item.QueueParser {
			jobID, err := upsertMemoryPostprocessJobTx(
				ctx,
				tx,
				domain.MemoryPostprocessJobEnqueue{
					IngestID:    ingestID,
					TenantID:    memory.TenantID,
					MemoryID:    memory.ID,
					JobType:     domain.PostprocessJobTypeParserExtract,
					MaxAttempts: maxAttempts,
				},
				nowRaw,
			)
			if err != nil {
				return domain.MemoryIngestReceipt{}, fmt.Errorf("queue parser job[%d]: %w", i, err)
			}
			if _, ok := seenJobs[jobID]; !ok {
				seenJobs[jobID] = struct{}{}
				jobIDs = append(jobIDs, jobID)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.MemoryIngestReceipt{}, fmt.Errorf("commit async-ingest transaction: %w", err)
	}

	return domain.MemoryIngestReceipt{
		IngestID:   ingestID,
		MemoryIDs:  memoryIDs,
		JobIDs:     jobIDs,
		AcceptedAt: now,
	}, nil
}

// EnqueuePostprocessJobs inserts postprocess jobs.
func (r *MemoryRepository) EnqueuePostprocessJobs(
	ctx context.Context,
	jobs []domain.MemoryPostprocessJobEnqueue,
	defaultMaxAttempts int,
) ([]domain.MemoryPostprocessJob, error) {
	if len(jobs) == 0 {
		return []domain.MemoryPostprocessJob{}, nil
	}
	if defaultMaxAttempts <= 0 {
		defaultMaxAttempts = 5
	}

	now := time.Now().UTC()
	nowRaw := now.Format(time.RFC3339Nano)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin enqueue-postprocess transaction: %w", err)
	}
	defer rollbackTx(tx)

	out := make([]domain.MemoryPostprocessJob, 0, len(jobs))
	for i := range jobs {
		job := jobs[i]
		if strings.TrimSpace(job.TenantID) == "" || strings.TrimSpace(job.MemoryID) == "" {
			return nil, domain.ErrInvalidInput
		}
		if !isPostprocessJobTypeSupported(job.JobType) {
			return nil, domain.ErrInvalidInput
		}
		if job.MaxAttempts <= 0 {
			job.MaxAttempts = defaultMaxAttempts
		}
		if strings.TrimSpace(job.IngestID) == "" {
			job.IngestID = newID("ing")
		}
		jobID, err := upsertMemoryPostprocessJobTx(ctx, tx, job, nowRaw)
		if err != nil {
			return nil, fmt.Errorf("enqueue postprocess job[%d]: %w", i, err)
		}
		out = append(out, domain.MemoryPostprocessJob{
			ID:          jobID,
			IngestID:    job.IngestID,
			TenantID:    job.TenantID,
			MemoryID:    job.MemoryID,
			JobType:     job.JobType,
			Status:      domain.PostprocessJobStatusQueued,
			Attempts:    0,
			MaxAttempts: job.MaxAttempts,
			AvailableAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit enqueue-postprocess transaction: %w", err)
	}
	return out, nil
}

// ClaimPostprocessJobs claims queued postprocess jobs.
func (r *MemoryRepository) ClaimPostprocessJobs(
	ctx context.Context,
	opts domain.MemoryPostprocessClaimOptions,
) ([]domain.MemoryPostprocessJob, error) {
	owner := strings.TrimSpace(opts.Owner)
	if owner == "" {
		return nil, domain.ErrInvalidInput
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	leaseUntil := opts.LeaseUntil.UTC()
	if leaseUntil.IsZero() || !leaseUntil.After(now) {
		leaseUntil = now.Add(30 * time.Second)
	}
	nowRaw := now.Format(time.RFC3339Nano)
	leaseRaw := leaseUntil.Format(time.RFC3339Nano)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim-postprocess transaction: %w", err)
	}
	defer rollbackTx(tx)

	rows, err := tx.QueryContext(ctx, ListMemoryPostprocessJobIDsForClaimSQL, nowRaw, nowRaw, limit)
	if err != nil {
		return nil, fmt.Errorf("select claimable postprocess jobs: %w", err)
	}
	defer closeRows(rows)

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan claimable postprocess id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimable postprocess ids: %w", err)
	}
	if len(ids) == 0 {
		return []domain.MemoryPostprocessJob{}, nil
	}

	claimedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		result, err := tx.ExecContext(ctx, MarkMemoryPostprocessJobClaimedSQL, owner, leaseRaw, nowRaw, id)
		if err != nil {
			return nil, fmt.Errorf("mark postprocess job claimed: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("postprocess claim rows affected: %w", err)
		}
		if affected > 0 {
			claimedIDs = append(claimedIDs, id)
		}
	}
	if len(claimedIDs) == 0 {
		return []domain.MemoryPostprocessJob{}, nil
	}

	jobs, err := listPostprocessJobsByIDsTx(ctx, tx, claimedIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim-postprocess transaction: %w", err)
	}
	return jobs, nil
}

// MarkPostprocessJobSucceeded marks a postprocess job as complete.
func (r *MemoryRepository) MarkPostprocessJobSucceeded(ctx context.Context, jobID string, now time.Time) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return domain.ErrInvalidInput
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, MarkMemoryPostprocessJobSucceededSQL, now.UTC().Format(time.RFC3339Nano), jobID)
	if err != nil {
		return fmt.Errorf("mark postprocess job succeeded: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postprocess success rows affected: %w", err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MarkPostprocessJobFailed records a failed postprocess attempt.
func (r *MemoryRepository) MarkPostprocessJobFailed(
	ctx context.Context,
	jobID string,
	now time.Time,
	nextAvailable time.Time,
	attempts int,
	status domain.PostprocessJobStatus,
	lastError string,
) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return domain.ErrInvalidInput
	}
	if status != domain.PostprocessJobStatusFailed && status != domain.PostprocessJobStatusDeadLetter {
		return domain.ErrInvalidInput
	}
	if attempts < 0 {
		return domain.ErrInvalidInput
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if nextAvailable.IsZero() {
		nextAvailable = now
	}
	result, err := r.db.ExecContext(
		ctx,
		MarkMemoryPostprocessJobFailedSQL,
		string(status),
		attempts,
		nextAvailable.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(lastError),
		now.UTC().Format(time.RFC3339Nano),
		jobID,
	)
	if err != nil {
		return fmt.Errorf("mark postprocess job failed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postprocess failed rows affected: %w", err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetPostprocessJob returns a postprocess job by ID.
func (r *MemoryRepository) GetPostprocessJob(ctx context.Context, jobID string) (*domain.MemoryPostprocessJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, domain.ErrInvalidInput
	}
	row := r.db.QueryRowContext(ctx, GetMemoryPostprocessJobByIDSQL, jobID)
	job, err := scanPostprocessJob(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get postprocess job: %w", err)
	}
	return &job, nil
}

// ListPostprocessJobs lists postprocess jobs using the provided filter.
func (r *MemoryRepository) ListPostprocessJobs(
	ctx context.Context,
	filter domain.MemoryPostprocessJobFilter,
) ([]domain.MemoryPostprocessJob, error) {
	query := ListMemoryPostprocessJobsBaseSQL + " WHERE 1=1"
	args := make([]any, 0, 12)

	tenantID := strings.TrimSpace(filter.TenantID)
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	statuses := uniqueValidPostprocessStatuses(filter.Statuses)
	if len(statuses) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
		query += " AND status IN (" + placeholders + ")"
		for _, status := range statuses {
			args = append(args, string(status))
		}
	}

	types := uniqueValidPostprocessTypes(filter.Types)
	if len(types) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(types)), ",")
		query += " AND job_type IN (" + placeholders + ")"
		for _, jobType := range types {
			args = append(args, string(jobType))
		}
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list postprocess jobs: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.MemoryPostprocessJob, 0, limit)
	for rows.Next() {
		job, err := scanPostprocessJob(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan postprocess job: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postprocess jobs: %w", err)
	}
	return out, nil
}

func upsertMemoryIndexJobTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, memoryID string,
	op domain.MemoryIndexOperation,
	state domain.MemoryIndexState,
	lastError string,
) error {
	if tx == nil {
		return domain.ErrInvalidInput
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	attempts := 0
	if state == domain.MemoryIndexStateFailed {
		attempts = 1
	}
	if _, err := tx.ExecContext(
		ctx,
		UpsertMemoryIndexJobSQL,
		newID("idx"),
		tenantID,
		memoryID,
		string(op),
		string(state),
		strings.TrimSpace(lastError),
		attempts,
		now,
		now,
	); err != nil {
		return err
	}
	return nil
}

func upsertMemoryPostprocessJobTx(
	ctx context.Context,
	tx *sql.Tx,
	job domain.MemoryPostprocessJobEnqueue,
	nowRaw string,
) (string, error) {
	if tx == nil {
		return "", domain.ErrInvalidInput
	}
	if strings.TrimSpace(job.TenantID) == "" || strings.TrimSpace(job.MemoryID) == "" {
		return "", domain.ErrInvalidInput
	}
	if !isPostprocessJobTypeSupported(job.JobType) {
		return "", domain.ErrInvalidInput
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	if strings.TrimSpace(job.IngestID) == "" {
		job.IngestID = newID("ing")
	}
	jobID := newID("ppj")
	if _, err := tx.ExecContext(
		ctx,
		UpsertMemoryPostprocessJobSQL,
		jobID,
		job.IngestID,
		job.TenantID,
		job.MemoryID,
		string(job.JobType),
		string(domain.PostprocessJobStatusQueued),
		0,
		job.MaxAttempts,
		nowRaw,
		"",
		"",
		"",
		nowRaw,
		nowRaw,
	); err != nil {
		return "", err
	}
	var resolvedID string
	if err := tx.QueryRowContext(
		ctx,
		GetMemoryPostprocessJobIDSQL,
		job.TenantID,
		job.MemoryID,
		string(job.JobType),
	).Scan(&resolvedID); err != nil {
		return "", err
	}
	return resolvedID, nil
}

func listPostprocessJobsByIDsTx(ctx context.Context, tx *sql.Tx, ids []string) ([]domain.MemoryPostprocessJob, error) {
	if tx == nil {
		return nil, domain.ErrInvalidInput
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		return []domain.MemoryPostprocessJob{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := ListMemoryPostprocessJobsBaseSQL + " WHERE id IN (" + placeholders + ")"
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query postprocess jobs by ids: %w", err)
	}
	defer closeRows(rows)

	loaded := make([]domain.MemoryPostprocessJob, 0, len(ids))
	for rows.Next() {
		job, err := scanPostprocessJob(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan postprocess job by ids: %w", err)
		}
		loaded = append(loaded, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postprocess jobs by ids: %w", err)
	}

	slices.SortFunc(loaded, func(a, b domain.MemoryPostprocessJob) int {
		return cmpIndex(ids, a.ID) - cmpIndex(ids, b.ID)
	})
	return loaded, nil
}

func scanPostprocessJob(scan func(dest ...any) error) (domain.MemoryPostprocessJob, error) {
	var (
		job          domain.MemoryPostprocessJob
		jobTypeRaw   string
		statusRaw    string
		availableRaw string
		leasedRaw    string
		createdRaw   string
		updatedRaw   string
	)
	if err := scan(
		&job.ID,
		&job.IngestID,
		&job.TenantID,
		&job.MemoryID,
		&jobTypeRaw,
		&statusRaw,
		&job.Attempts,
		&job.MaxAttempts,
		&availableRaw,
		&job.LeaseOwner,
		&leasedRaw,
		&job.LastError,
		&createdRaw,
		&updatedRaw,
	); err != nil {
		return domain.MemoryPostprocessJob{}, err
	}
	job.JobType = domain.PostprocessJobType(jobTypeRaw)
	job.Status = domain.PostprocessJobStatus(statusRaw)

	var err error
	job.AvailableAt, err = parseStoredTime(availableRaw)
	if err != nil {
		return domain.MemoryPostprocessJob{}, fmt.Errorf("parse postprocess available_at: %w", err)
	}
	job.LeasedUntil, err = parseOptionalStoredTime(leasedRaw)
	if err != nil {
		return domain.MemoryPostprocessJob{}, fmt.Errorf("parse postprocess leased_until: %w", err)
	}
	job.CreatedAt, err = parseStoredTime(createdRaw)
	if err != nil {
		return domain.MemoryPostprocessJob{}, fmt.Errorf("parse postprocess created_at: %w", err)
	}
	job.UpdatedAt, err = parseStoredTime(updatedRaw)
	if err != nil {
		return domain.MemoryPostprocessJob{}, fmt.Errorf("parse postprocess updated_at: %w", err)
	}
	return job, nil
}

func parseStoredTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, raw)
}

func parseOptionalStoredTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return parseStoredTime(raw)
}

func uniqueValidPostprocessStatuses(in []domain.PostprocessJobStatus) []domain.PostprocessJobStatus {
	if len(in) == 0 {
		return []domain.PostprocessJobStatus{}
	}
	out := make([]domain.PostprocessJobStatus, 0, len(in))
	seen := make(map[domain.PostprocessJobStatus]struct{}, len(in))
	for _, status := range in {
		if !isPostprocessStatusSupported(status) {
			continue
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out
}

func uniqueValidPostprocessTypes(in []domain.PostprocessJobType) []domain.PostprocessJobType {
	if len(in) == 0 {
		return []domain.PostprocessJobType{}
	}
	out := make([]domain.PostprocessJobType, 0, len(in))
	seen := make(map[domain.PostprocessJobType]struct{}, len(in))
	for _, jobType := range in {
		if !isPostprocessJobTypeSupported(jobType) {
			continue
		}
		if _, ok := seen[jobType]; ok {
			continue
		}
		seen[jobType] = struct{}{}
		out = append(out, jobType)
	}
	return out
}

func isPostprocessStatusSupported(status domain.PostprocessJobStatus) bool {
	switch status {
	case domain.PostprocessJobStatusQueued,
		domain.PostprocessJobStatusRunning,
		domain.PostprocessJobStatusSucceeded,
		domain.PostprocessJobStatusFailed,
		domain.PostprocessJobStatusDeadLetter:
		return true
	default:
		return false
	}
}

func isPostprocessJobTypeSupported(jobType domain.PostprocessJobType) bool {
	switch jobType {
	case domain.PostprocessJobTypeParserExtract, domain.PostprocessJobTypeVectorUpsert:
		return true
	default:
		return false
	}
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
