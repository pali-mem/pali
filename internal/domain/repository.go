package domain

import (
	"context"
	"time"
)

type MemoryRepository interface {
	Store(ctx context.Context, m Memory) (Memory, error)
	Delete(ctx context.Context, tenantID, memoryID string) error
	Search(ctx context.Context, tenantID, query string, topK int) ([]Memory, error)
	GetByIDs(ctx context.Context, tenantID string, ids []string) ([]Memory, error)
	Touch(ctx context.Context, tenantID string, ids []string) error
}

// MemoryCountRepository is an optional extension for repositories that can
// return a total count across all tenants.
type MemoryCountRepository interface {
	Count(ctx context.Context) (int64, error)
}

type MemorySearchFilters struct {
	Tiers []MemoryTier
	Kinds []MemoryKind
}

// MemoryFilteredSearchRepository is an optional extension for repositories
// that can apply tier/kind constraints during lexical retrieval.
type MemoryFilteredSearchRepository interface {
	SearchWithFilters(
		ctx context.Context,
		tenantID, query string,
		topK int,
		filters MemorySearchFilters,
	) ([]Memory, error)
}

// MemoryBatchRepository is an optional extension for repositories that can
// persist multiple memories in one transaction.
type MemoryBatchRepository interface {
	StoreBatch(ctx context.Context, memories []Memory) ([]Memory, error)
}

// MemoryCanonicalKeyRepository is an optional extension for repositories that
// can look up a memory by a deterministic canonical identity.
type MemoryCanonicalKeyRepository interface {
	FindByCanonicalKey(ctx context.Context, tenantID, canonicalKey string) (*Memory, error)
}

// MemorySourceTurnRepository is an optional extension for repositories that
// can list memories grounded to the same source turn.
type MemorySourceTurnRepository interface {
	ListBySourceTurnHash(ctx context.Context, tenantID, sourceTurnHash string, limit int) ([]Memory, error)
}

type MemoryIndexOperation string

const (
	MemoryIndexOperationUpsert MemoryIndexOperation = "upsert"
	MemoryIndexOperationDelete MemoryIndexOperation = "delete"
)

type MemoryIndexState string

const (
	MemoryIndexStatePending    MemoryIndexState = "pending"
	MemoryIndexStateIndexed    MemoryIndexState = "indexed"
	MemoryIndexStateFailed     MemoryIndexState = "failed"
	MemoryIndexStateTombstoned MemoryIndexState = "tombstoned"
)

// MemoryIndexStateRepository is an optional extension for repositories that
// persist index job state alongside memory metadata.
type MemoryIndexStateRepository interface {
	MarkIndexState(
		ctx context.Context,
		tenantID string,
		memoryIDs []string,
		op MemoryIndexOperation,
		state MemoryIndexState,
		lastError string,
	) error
}

type PostprocessJobType string

const (
	PostprocessJobTypeParserExtract PostprocessJobType = "parser_extract"
	PostprocessJobTypeVectorUpsert  PostprocessJobType = "vector_upsert"
)

type PostprocessJobStatus string

const (
	PostprocessJobStatusQueued     PostprocessJobStatus = "queued"
	PostprocessJobStatusRunning    PostprocessJobStatus = "running"
	PostprocessJobStatusSucceeded  PostprocessJobStatus = "succeeded"
	PostprocessJobStatusFailed     PostprocessJobStatus = "failed"
	PostprocessJobStatusDeadLetter PostprocessJobStatus = "dead_letter"
)

type MemoryPostprocessJob struct {
	ID          string
	IngestID    string
	TenantID    string
	MemoryID    string
	JobType     PostprocessJobType
	Status      PostprocessJobStatus
	Attempts    int
	MaxAttempts int
	AvailableAt time.Time
	LeaseOwner  string
	LeasedUntil time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MemoryPostprocessJobFilter struct {
	TenantID string
	Statuses []PostprocessJobStatus
	Types    []PostprocessJobType
	Limit    int
}

type MemoryPostprocessJobEnqueue struct {
	IngestID    string
	TenantID    string
	MemoryID    string
	JobType     PostprocessJobType
	MaxAttempts int
}

type MemoryPostprocessClaimOptions struct {
	Owner      string
	Limit      int
	Now        time.Time
	LeaseUntil time.Time
}

type MemoryAsyncIngestItem struct {
	Memory      Memory
	QueueParser bool
	QueueVector bool
}

type MemoryIngestReceipt struct {
	IngestID   string
	MemoryIDs  []string
	JobIDs     []string
	AcceptedAt time.Time
}

// MemoryAsyncIngestRepository is an optional extension for repositories that
// can write memories and enqueue postprocess jobs atomically.
type MemoryAsyncIngestRepository interface {
	StoreBatchAsyncIngest(
		ctx context.Context,
		items []MemoryAsyncIngestItem,
		maxAttempts int,
	) (MemoryIngestReceipt, error)
}

// MemoryPostprocessJobRepository is an optional extension for repositories that
// expose postprocess job queue operations.
type MemoryPostprocessJobRepository interface {
	EnqueuePostprocessJobs(
		ctx context.Context,
		jobs []MemoryPostprocessJobEnqueue,
		defaultMaxAttempts int,
	) ([]MemoryPostprocessJob, error)
	ClaimPostprocessJobs(
		ctx context.Context,
		opts MemoryPostprocessClaimOptions,
	) ([]MemoryPostprocessJob, error)
	MarkPostprocessJobSucceeded(ctx context.Context, jobID string, now time.Time) error
	MarkPostprocessJobFailed(
		ctx context.Context,
		jobID string,
		now time.Time,
		nextAvailable time.Time,
		attempts int,
		status PostprocessJobStatus,
		lastError string,
	) error
	GetPostprocessJob(ctx context.Context, jobID string) (*MemoryPostprocessJob, error)
	ListPostprocessJobs(ctx context.Context, filter MemoryPostprocessJobFilter) ([]MemoryPostprocessJob, error)
}

type EntityFactRepository interface {
	Store(ctx context.Context, fact EntityFact) (EntityFact, error)
	ListByEntityRelation(ctx context.Context, tenantID, entity, relation string, limit int) ([]EntityFact, error)
}

type EntityFactBatchRepository interface {
	StoreBatch(ctx context.Context, facts []EntityFact) ([]EntityFact, error)
}

// EntityFactInvalidationRepository is an optional extension for repositories
// that can close out older singleton facts when a newer canonical fact wins.
type EntityFactInvalidationRepository interface {
	InvalidateEntityRelation(
		ctx context.Context,
		tenantID, entity, relation, activeValue, invalidatedByFactID string,
		validTo time.Time,
	) error
}

// EntityFactGraphRepository is an optional extension for repositories that
// can traverse graph neighborhoods around seed entities.
type EntityFactGraphRepository interface {
	ListByEntityNeighborhood(ctx context.Context, tenantID string, seeds []string, limit int) ([]EntityFact, error)
}

// EntityFactPathRepository is an optional extension for repositories that
// can return path-aware graph candidates for multi-hop retrieval.
type EntityFactPathRepository interface {
	ListByEntityPaths(ctx context.Context, tenantID string, query EntityFactPathQuery) ([]EntityFactPathCandidate, error)
}

type TenantRepository interface {
	Create(ctx context.Context, t Tenant) (Tenant, error)
	Exists(ctx context.Context, tenantID string) (bool, error)
	MemoryCount(ctx context.Context, tenantID string) (int64, error)
	List(ctx context.Context, limit int) ([]Tenant, error)
}

// TenantMemoryCountsRepository is an optional extension for repositories that
// can return per-tenant memory totals in one call.
type TenantMemoryCountsRepository interface {
	ListMemoryCounts(ctx context.Context, tenantIDs []string) (map[string]int64, error)
}

// TenantCountRepository is an optional extension for repositories that can
// return a total tenant count.
type TenantCountRepository interface {
	Count(ctx context.Context) (int64, error)
}
