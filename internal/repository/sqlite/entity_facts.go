package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
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
		fact.RelationRaw,
		fact.Value,
		nullString(fact.MemoryID),
		formatTimeOrEmpty(fact.ObservedAt),
		formatTimeOrEmpty(fact.ValidFrom),
		formatTimeOrEmptyPtr(fact.ValidTo),
		strings.TrimSpace(fact.InvalidatedByFactID),
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
			fact.RelationRaw,
			fact.Value,
			nullString(fact.MemoryID),
			formatTimeOrEmpty(fact.ObservedAt),
			formatTimeOrEmpty(fact.ValidFrom),
			formatTimeOrEmptyPtr(fact.ValidTo),
			strings.TrimSpace(fact.InvalidatedByFactID),
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
			observedAt   string
			validFrom    string
			validTo      string
		)
		if err := rows.Scan(
			&fact.ID,
			&fact.TenantID,
			&fact.Entity,
			&fact.Relation,
			&fact.RelationRaw,
			&fact.Value,
			&memoryIDRaw,
			&observedAt,
			&validFrom,
			&validTo,
			&fact.InvalidatedByFactID,
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
		fact.ObservedAt = parseTimeOrZero(observedAt)
		fact.ValidFrom = parseTimeOrZero(validFrom)
		if parsedValidTo := parseTimeOrZero(validTo); !parsedValidTo.IsZero() {
			fact.ValidTo = &parsedValidTo
		}
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
	fact.RelationRaw = normalizeEntityFactRawRelation(fact.RelationRaw)
	fact.Value = normalizeEntityFactValue(fact.Value)
	fact.MemoryID = strings.TrimSpace(fact.MemoryID)
	if fact.TenantID == "" || fact.Entity == "" || fact.Relation == "" || fact.Value == "" {
		return domain.ErrInvalidInput
	}
	if fact.RelationRaw == "" {
		fact.RelationRaw = fact.Relation
	}
	if fact.ID == "" {
		fact.ID = newID("ef")
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = now
	}
	if fact.ObservedAt.IsZero() {
		fact.ObservedAt = fact.CreatedAt
	}
	if fact.ValidFrom.IsZero() {
		fact.ValidFrom = fact.ObservedAt
	}
	return nil
}

func (r *EntityFactRepository) InvalidateEntityRelation(
	ctx context.Context,
	tenantID, entity, relation, activeValue, invalidatedByFactID string,
	validTo time.Time,
) error {
	tenantID = strings.TrimSpace(tenantID)
	entity = normalizeEntityFactKey(entity)
	relation = normalizeEntityFactKey(relation)
	activeValue = normalizeEntityFactValue(activeValue)
	invalidatedByFactID = strings.TrimSpace(invalidatedByFactID)
	if tenantID == "" || entity == "" || relation == "" || activeValue == "" || invalidatedByFactID == "" || validTo.IsZero() {
		return domain.ErrInvalidInput
	}
	_, err := r.db.ExecContext(
		ctx,
		InvalidateEntityFactsByRelationSQL,
		validTo.UTC().Format(time.RFC3339Nano),
		invalidatedByFactID,
		tenantID,
		entity,
		relation,
		activeValue,
	)
	if err != nil {
		return fmt.Errorf("invalidate entity facts by relation: %w", err)
	}
	return nil
}

func normalizeEntityFactKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeEntityFactValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeEntityFactRawRelation(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func formatTimeOrEmpty(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatTimeOrEmptyPtr(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimeOrZero(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
