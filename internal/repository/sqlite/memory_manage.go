package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

// SQL statements for paged listing and partial updates of memories.
const (
	listMemoriesPageSQL = `
SELECT id, tenant_id, content, query_view_text, tier, tags_json, source, created_by, kind, canonical_key, source_turn_hash, source_fact_index, extractor, extractor_version, importance_score, recall_count, metadata_json, created_at, updated_at, last_accessed_at, last_recalled_at
FROM memories
WHERE tenant_id = ?
ORDER BY julianday(created_at) DESC, rowid DESC
LIMIT ? OFFSET ?
`

	updateMemorySQL = `
UPDATE memories
SET content = ?,
	query_view_text = ?,
	tier = ?,
	tags_json = ?,
	importance_score = ?,
	canonical_key = ?,
	source_turn_hash = ?,
	updated_at = ?
WHERE tenant_id = ? AND id = ?
`

	tenantMemoryTierCountsSQL = `
SELECT tier, COUNT(1) FROM memories WHERE tenant_id = ? GROUP BY tier
`

	tenantMemoryKindCountsSQL = `
SELECT kind, COUNT(1) FROM memories WHERE tenant_id = ? GROUP BY kind
`

	tenantMemoryRecallTotalSQL = `
SELECT COALESCE(SUM(recall_count), 0) FROM memories WHERE tenant_id = ?
`

	tenantNewestMemorySQL = `
SELECT created_at FROM memories WHERE tenant_id = ? ORDER BY julianday(created_at) DESC, rowid DESC LIMIT 1
`

	getTenantSQL = `
SELECT id, name, created_at FROM tenants WHERE id = ?
`
)

// ListPage returns one page of a tenant's memories, newest first.
func (r *MemoryRepository) ListPage(
	ctx context.Context,
	tenantID string,
	filter domain.MemoryListFilter,
) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, domain.ErrInvalidInput
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	sqlQuery, filterArgs := appendMemoryFilterClause(listMemoriesPageSQL, "", domain.MemorySearchFilters{
		Tiers: filter.Tiers,
		Kinds: filter.Kinds,
	})
	args := make([]any, 0, len(filterArgs)+3)
	args = append(args, tenantID)
	args = append(args, filterArgs...)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list memories page: %w", err)
	}
	defer closeRows(rows)

	memories := make([]domain.Memory, 0, filter.Limit)
	for rows.Next() {
		m, err := scanMemory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan memory page row: %w", err)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory page rows: %w", err)
	}
	return memories, nil
}

// Update applies a partial update to one memory and, when a content value is
// supplied, rewrites its FTS row and marks the vector index pending in the same transaction.
func (r *MemoryRepository) Update(
	ctx context.Context,
	tenantID, memoryID string,
	patch domain.MemoryPatch,
) (domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.Memory{}, domain.ErrInvalidInput
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("begin update transaction: %w", err)
	}
	defer rollbackTx(tx)

	row := tx.QueryRowContext(ctx, GetMemoriesByIDsBaseSQL+" AND id = ?", tenantID, memoryID)
	m, err := scanMemory(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Memory{}, domain.ErrNotFound
		}
		return domain.Memory{}, fmt.Errorf("load memory for update: %w", err)
	}

	contentChanged := patch.Content != nil
	if contentChanged {
		m.Content = *patch.Content
		// The query view was generated from the old content; keeping it would
		// leave the old text searchable through FTS and the embedding.
		m.QueryViewText = ""
	}
	if patch.Tags != nil {
		m.Tags = append([]string{}, *patch.Tags...)
	}
	if patch.Tier != nil {
		m.Tier = *patch.Tier
	}
	if patch.Importance != nil {
		m.Importance = *patch.Importance
	}
	if patch.CanonicalKey != nil {
		m.CanonicalKey = *patch.CanonicalKey
	}
	if patch.SourceTurnHash != nil {
		m.SourceTurnHash = *patch.SourceTurnHash
	}
	m.UpdatedAt = time.Now().UTC()

	tagsJSON, err := json.Marshal(m.Tags)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("marshal tags: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		updateMemorySQL,
		m.Content,
		m.QueryViewText,
		string(m.Tier),
		string(tagsJSON),
		m.Importance,
		m.CanonicalKey,
		m.SourceTurnHash,
		m.UpdatedAt.Format(time.RFC3339Nano),
		tenantID,
		memoryID,
	); err != nil {
		return domain.Memory{}, fmt.Errorf("update memory: %w", err)
	}

	if contentChanged {
		if _, err := tx.ExecContext(ctx, DeleteMemoryFTSSQL, tenantID, memoryID); err != nil {
			return domain.Memory{}, fmt.Errorf("delete memory fts row: %w", err)
		}
		if _, err := tx.ExecContext(ctx, InsertMemoryFTSSQL, buildIndexedMemoryText(m), tenantID, memoryID); err != nil {
			return domain.Memory{}, fmt.Errorf("insert memory fts row: %w", err)
		}
		if err := upsertMemoryIndexJobTx(
			ctx,
			tx,
			tenantID,
			memoryID,
			domain.MemoryIndexOperationUpsert,
			domain.MemoryIndexStatePending,
			"",
		); err != nil {
			return domain.Memory{}, fmt.Errorf("queue upsert index job: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.Memory{}, fmt.Errorf("commit update transaction: %w", err)
	}
	return m, nil
}

// Get returns one tenant record.
func (r *TenantRepository) Get(ctx context.Context, tenantID string) (domain.Tenant, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.Tenant{}, domain.ErrInvalidInput
	}
	var (
		t            domain.Tenant
		createdAtRaw string
	)
	if err := r.db.QueryRowContext(ctx, getTenantSQL, tenantID).Scan(&t.ID, &t.Name, &createdAtRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Tenant{}, domain.ErrNotFound
		}
		return domain.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("parse tenant created_at: %w", err)
	}
	t.CreatedAt = createdAt
	return t, nil
}

// MemoryStats returns tier and kind counts, total recalls, and the newest
// memory creation time for a tenant.
func (r *TenantRepository) MemoryStats(ctx context.Context, tenantID string) (domain.TenantMemoryStats, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.TenantMemoryStats{}, domain.ErrInvalidInput
	}
	out := domain.TenantMemoryStats{
		ByTier: map[string]int64{},
		ByKind: map[string]int64{},
	}
	if err := r.scanCounts(ctx, tenantMemoryTierCountsSQL, tenantID, out.ByTier); err != nil {
		return domain.TenantMemoryStats{}, fmt.Errorf("count memories by tier: %w", err)
	}
	if err := r.scanCounts(ctx, tenantMemoryKindCountsSQL, tenantID, out.ByKind); err != nil {
		return domain.TenantMemoryStats{}, fmt.Errorf("count memories by kind: %w", err)
	}
	for _, n := range out.ByTier {
		out.MemoryCount += n
	}
	if err := r.db.QueryRowContext(ctx, tenantMemoryRecallTotalSQL, tenantID).Scan(&out.TotalRecalls); err != nil {
		return domain.TenantMemoryStats{}, fmt.Errorf("sum memory recalls: %w", err)
	}
	var newestRaw string
	err := r.db.QueryRowContext(ctx, tenantNewestMemorySQL, tenantID).Scan(&newestRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return domain.TenantMemoryStats{}, fmt.Errorf("newest memory time: %w", err)
	default:
		out.NewestMemoryAt, err = time.Parse(time.RFC3339Nano, newestRaw)
		if err != nil {
			return domain.TenantMemoryStats{}, fmt.Errorf("parse newest memory time: %w", err)
		}
	}
	return out, nil
}

func (r *TenantRepository) scanCounts(ctx context.Context, query, tenantID string, into map[string]int64) error {
	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return err
	}
	defer closeRows(rows)
	for rows.Next() {
		var (
			key   string
			count int64
		)
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		into[key] = count
	}
	return rows.Err()
}
