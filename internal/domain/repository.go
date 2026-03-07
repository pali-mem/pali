package domain

import "context"

type MemoryRepository interface {
	Store(ctx context.Context, m Memory) (Memory, error)
	Delete(ctx context.Context, tenantID, memoryID string) error
	Search(ctx context.Context, tenantID, query string, topK int) ([]Memory, error)
	GetByIDs(ctx context.Context, tenantID string, ids []string) ([]Memory, error)
	Touch(ctx context.Context, tenantID string, ids []string) error
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

type EntityFactRepository interface {
	Store(ctx context.Context, fact EntityFact) (EntityFact, error)
	ListByEntityRelation(ctx context.Context, tenantID, entity, relation string, limit int) ([]EntityFact, error)
}

type EntityFactBatchRepository interface {
	StoreBatch(ctx context.Context, facts []EntityFact) ([]EntityFact, error)
}

type TenantRepository interface {
	Create(ctx context.Context, t Tenant) (Tenant, error)
	Exists(ctx context.Context, tenantID string) (bool, error)
	MemoryCount(ctx context.Context, tenantID string) (int64, error)
	List(ctx context.Context, limit int) ([]Tenant, error)
}
