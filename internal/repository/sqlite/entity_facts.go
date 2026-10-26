package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

// EntityFactRepository stores and queries normalized entity facts in SQLite.
type EntityFactRepository struct {
	db *sql.DB
}

// NewEntityFactRepository builds a SQLite-backed entity fact repository.
func NewEntityFactRepository(db *sql.DB) *EntityFactRepository {
	return &EntityFactRepository{db: db}
}

// Store inserts a single entity fact.
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

// StoreBatch inserts a batch of entity facts in one transaction.
func (r *EntityFactRepository) StoreBatch(ctx context.Context, facts []domain.EntityFact) ([]domain.EntityFact, error) {
	if len(facts) == 0 {
		return []domain.EntityFact{}, nil
	}

	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entity fact batch transaction: %w", err)
	}
	defer rollbackTx(tx)

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

// ListByEntityRelation returns facts for an entity/relation pair.
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
	defer closeRows(rows)

	out := make([]domain.EntityFact, 0, limit)
	for rows.Next() {
		fact, err := scanEntityFactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity fact rows: %w", err)
	}
	return out, nil
}

// entityFactLookupPairsPerQuery keeps each compound SELECT well under
// SQLite's default SQLITE_MAX_COMPOUND_SELECT (500).
const entityFactLookupPairsPerQuery = 100

type entityRelationPair struct{ entity, relation string }

// ListByEntityRelations returns the facts ListByEntityRelation would return
// for every entity (outer) and relation (inner) pair, concatenated in that
// order. Each distinct pair runs the single-pair query as one arm of a
// UNION ALL, so per-pair limits and ordering are unchanged while a lookup
// costs one round trip per 100 pairs instead of one per pair.
func (r *EntityFactRepository) ListByEntityRelations(
	ctx context.Context,
	tenantID string,
	entities, relations []string,
	limit int,
) ([]domain.EntityFact, error) {
	if len(entities) == 0 || len(relations) == 0 {
		return []domain.EntityFact{}, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, domain.ErrInvalidInput
	}
	if limit <= 0 {
		limit = 100
	}

	requested := make([]entityRelationPair, 0, len(entities)*len(relations))
	unique := make([]entityRelationPair, 0, len(entities)*len(relations))
	indexByPair := make(map[entityRelationPair]int, len(entities)*len(relations))
	for _, entity := range entities {
		entity = normalizeEntityFactKey(entity)
		for _, relation := range relations {
			relation = normalizeEntityFactKey(relation)
			if entity == "" || relation == "" {
				return nil, domain.ErrInvalidInput
			}
			key := entityRelationPair{entity: entity, relation: relation}
			requested = append(requested, key)
			if _, ok := indexByPair[key]; !ok {
				indexByPair[key] = len(unique)
				unique = append(unique, key)
			}
		}
	}

	factsByPair := make([][]domain.EntityFact, len(unique))
	for start := 0; start < len(unique); start += entityFactLookupPairsPerQuery {
		end := min(start+entityFactLookupPairsPerQuery, len(unique))
		if err := r.listEntityFactPairs(ctx, tenantID, limit, start, unique[start:end], factsByPair); err != nil {
			return nil, err
		}
	}

	out := make([]domain.EntityFact, 0, len(requested))
	for _, key := range requested {
		out = append(out, factsByPair[indexByPair[key]]...)
	}
	return out, nil
}

// listEntityFactPairs runs one UNION ALL query for pairs and stores each
// pair's facts at factsByPair[offset+i].
func (r *EntityFactRepository) listEntityFactPairs(
	ctx context.Context,
	tenantID string,
	limit, offset int,
	pairs []entityRelationPair,
	factsByPair [][]domain.EntityFact,
) error {
	arms := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*4)
	for i, pair := range pairs {
		arms = append(arms, fmt.Sprintf("SELECT %d AS pair_idx, * FROM (%s)", offset+i, ListEntityFactsByEntityRelationSQL))
		args = append(args, tenantID, pair.entity, pair.relation, limit)
	}
	rows, err := r.db.QueryContext(ctx, strings.Join(arms, " UNION ALL "), args...)
	if err != nil {
		return fmt.Errorf("list entity facts by relations: %w", err)
	}
	defer closeRows(rows)
	for rows.Next() {
		var pairIdx int
		fact, err := scanEntityFactRow(rows, &pairIdx)
		if err != nil {
			return err
		}
		if pairIdx < offset || pairIdx >= offset+len(pairs) {
			return fmt.Errorf("list entity facts by relations: unexpected pair index %d", pairIdx)
		}
		factsByPair[pairIdx] = append(factsByPair[pairIdx], fact)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate entity fact rows: %w", err)
	}
	return nil
}

// scanEntityFactRow scans one entity_facts row. leading receives any columns
// selected before the standard entity fact columns.
func scanEntityFactRow(rows *sql.Rows, leading ...any) (domain.EntityFact, error) {
	var (
		fact         domain.EntityFact
		createdAtRaw string
		memoryIDRaw  sql.NullString
		observedAt   string
		validFrom    string
		validTo      string
	)
	dest := append(leading,
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
	)
	if err := rows.Scan(dest...); err != nil {
		return domain.EntityFact{}, fmt.Errorf("scan entity fact row: %w", err)
	}
	fact.MemoryID = strings.TrimSpace(memoryIDRaw.String)
	parsedTime, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.EntityFact{}, fmt.Errorf("parse entity fact created_at: %w", err)
	}
	fact.CreatedAt = parsedTime
	fact.ObservedAt = parseTimeOrZero(observedAt)
	fact.ValidFrom = parseTimeOrZero(validFrom)
	if parsedValidTo := parseTimeOrZero(validTo); !parsedValidTo.IsZero() {
		fact.ValidTo = &parsedValidTo
	}
	return fact, nil
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

// InvalidateEntityRelation marks older facts as invalidated.
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

// DeleteByMemoryIDs removes the entity facts sourced from the given memories.
func (r *EntityFactRepository) DeleteByMemoryIDs(ctx context.Context, tenantID string, memoryIDs []string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return domain.ErrInvalidInput
	}
	if len(memoryIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(memoryIDs)+1)
	args = append(args, tenantID)
	for _, id := range memoryIDs {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(memoryIDs)), ",")
	query := "DELETE FROM entity_facts WHERE tenant_id = ? AND memory_id IN (" + placeholders + ")"
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete entity facts by memory: %w", err)
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
