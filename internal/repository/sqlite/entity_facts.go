package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vein05/pali/internal/domain"
)

type EntityFactRepository struct {
	db *sql.DB
}

func NewEntityFactRepository(db *sql.DB) *EntityFactRepository {
	return &EntityFactRepository{db: db}
}

func (r *EntityFactRepository) Store(ctx context.Context, fact domain.EntityFact) (domain.EntityFact, error) {
	now := time.Now().UTC()
	if err := prepareEntityFactForStore(&fact, now); err != nil {
		return domain.EntityFact{}, err
	}
	if _, err := r.db.ExecContext(
		ctx,
		InsertEntityFactSQL,
		fact.ID,
		fact.TenantID,
		fact.Entity,
		fact.Relation,
		fact.Value,
		nullString(fact.MemoryID),
		fact.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return domain.EntityFact{}, fmt.Errorf("insert entity fact: %w", err)
	}
	return fact, nil
}

func (r *EntityFactRepository) StoreBatch(ctx context.Context, facts []domain.EntityFact) ([]domain.EntityFact, error) {
	if len(facts) == 0 {
		return []domain.EntityFact{}, nil
	}

	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entity fact batch transaction: %w", err)
	}
	defer tx.Rollback()

	out := make([]domain.EntityFact, 0, len(facts))
	for i := range facts {
		fact := facts[i]
		if err := prepareEntityFactForStore(&fact, now); err != nil {
			return nil, fmt.Errorf("prepare entity fact[%d]: %w", i, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			InsertEntityFactSQL,
			fact.ID,
			fact.TenantID,
			fact.Entity,
			fact.Relation,
			fact.Value,
			nullString(fact.MemoryID),
			fact.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return nil, fmt.Errorf("insert entity fact[%d]: %w", i, err)
		}
		out = append(out, fact)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entity fact batch transaction: %w", err)
	}
	return out, nil
}

func (r *EntityFactRepository) ListByEntityRelation(
	ctx context.Context,
	tenantID, entity, relation string,
	limit int,
) ([]domain.EntityFact, error) {
	tenantID = strings.TrimSpace(tenantID)
	entity = normalizeEntityFactKey(entity)
	relation = normalizeEntityFactKey(relation)
	if tenantID == "" || entity == "" || relation == "" {
		return nil, domain.ErrInvalidInput
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx, ListEntityFactsByEntityRelationSQL, tenantID, entity, relation, limit)
	if err != nil {
		return nil, fmt.Errorf("list entity facts by relation: %w", err)
	}
	defer rows.Close()

	out := make([]domain.EntityFact, 0, limit)
	for rows.Next() {
		var (
			fact         domain.EntityFact
			createdAtRaw string
			memoryIDRaw  sql.NullString
		)
		if err := rows.Scan(
			&fact.ID,
			&fact.TenantID,
			&fact.Entity,
			&fact.Relation,
			&fact.Value,
			&memoryIDRaw,
			&createdAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan entity fact row: %w", err)
		}
		fact.MemoryID = strings.TrimSpace(memoryIDRaw.String)
		parsedTime, err := time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse entity fact created_at: %w", err)
		}
		fact.CreatedAt = parsedTime
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity fact rows: %w", err)
	}
	return out, nil
}

func prepareEntityFactForStore(fact *domain.EntityFact, now time.Time) error {
	if fact == nil {
		return domain.ErrInvalidInput
	}
	fact.TenantID = strings.TrimSpace(fact.TenantID)
	fact.Entity = normalizeEntityFactKey(fact.Entity)
	fact.Relation = normalizeEntityFactKey(fact.Relation)
	fact.Value = normalizeEntityFactValue(fact.Value)
	fact.MemoryID = strings.TrimSpace(fact.MemoryID)
	if fact.TenantID == "" || fact.Entity == "" || fact.Relation == "" || fact.Value == "" {
		return domain.ErrInvalidInput
	}
	if fact.ID == "" {
		fact.ID = newID("ef")
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = now
	}
	return nil
}

func normalizeEntityFactKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeEntityFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
