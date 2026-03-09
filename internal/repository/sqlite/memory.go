package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

var (
	ftsTokenPattern         = regexp.MustCompile(`[a-zA-Z0-9_]+`)
	indexedTagPattern       = regexp.MustCompile(`\[[^\]]+\]`)
	indexedSpeakerPrefix    = regexp.MustCompile(`^\s*[A-Za-z][A-Za-z0-9 .'\-]{0,80}:\s*`)
	indexedSpeakerCapture   = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 .'\-]{0,80}):\s*`)
	indexedSpeakerTag       = regexp.MustCompile(`(?i)\[speaker_[ab]:([^\]]+)\]`)
	ftsQueryStopwordPattern = map[string]struct{}{
		"a": {}, "an": {}, "the": {}, "what": {}, "when": {}, "where": {}, "who": {}, "why": {}, "how": {}, "which": {}, "whose": {},
		"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "to": {}, "of": {}, "in": {}, "on": {}, "at": {},
		"for": {}, "with": {}, "about": {}, "tell": {}, "me": {}, "it": {}, "this": {}, "that": {},
	}
)

const maxFTSQueryTokens = 12

type MemoryRepository struct {
	db *sql.DB
}

func NewMemoryRepository(db *sql.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

func (r *MemoryRepository) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	if strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(m.Content) == "" {
		return domain.Memory{}, domain.ErrInvalidInput
	}
	now := time.Now().UTC()
	if err := prepareMemoryForStore(&m, now); err != nil {
		return domain.Memory{}, err
	}

	tagsJSON, err := json.Marshal(m.Tags)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("marshal tags: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("begin store transaction: %w", err)
	}
	defer tx.Rollback()

	if err := insertMemoryTx(ctx, tx, m, string(tagsJSON)); err != nil {
		return domain.Memory{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Memory{}, fmt.Errorf("commit store transaction: %w", err)
	}

	return m, nil
}

func (r *MemoryRepository) StoreBatch(ctx context.Context, memories []domain.Memory) ([]domain.Memory, error) {
	if len(memories) == 0 {
		return []domain.Memory{}, nil
	}

	now := time.Now().UTC()
	stored := make([]domain.Memory, 0, len(memories))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin store-batch transaction: %w", err)
	}
	defer tx.Rollback()

	for i := range memories {
		m := memories[i]
		if err := prepareMemoryForStore(&m, now); err != nil {
			return nil, fmt.Errorf("prepare memory[%d]: %w", i, err)
		}

		tagsJSON, err := json.Marshal(m.Tags)
		if err != nil {
			return nil, fmt.Errorf("marshal tags for memory[%d]: %w", i, err)
		}
		if err := insertMemoryTx(ctx, tx, m, string(tagsJSON)); err != nil {
			return nil, fmt.Errorf("store memory[%d]: %w", i, err)
		}
		stored = append(stored, m)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit store-batch transaction: %w", err)
	}
	return stored, nil
}

func (r *MemoryRepository) Delete(ctx context.Context, tenantID, memoryID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete transaction: %w", err)
	}
	defer tx.Rollback()

	if err := upsertMemoryIndexJobTx(
		ctx,
		tx,
		tenantID,
		memoryID,
		domain.MemoryIndexOperationDelete,
		domain.MemoryIndexStatePending,
		"",
	); err != nil {
		return fmt.Errorf("queue delete index job: %w", err)
	}

	if _, err := tx.ExecContext(ctx, DeleteMemoryFTSSQL, tenantID, memoryID); err != nil {
		return fmt.Errorf("delete memory fts row: %w", err)
	}

	result, err := tx.ExecContext(ctx, DeleteMemorySQL, tenantID, memoryID)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete memory rows affected: %w", err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete transaction: %w", err)
	}
	return nil
}

func (r *MemoryRepository) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return r.SearchWithFilters(ctx, tenantID, query, topK, domain.MemorySearchFilters{})
}

func (r *MemoryRepository) SearchWithFilters(
	ctx context.Context,
	tenantID, query string,
	topK int,
	filters domain.MemorySearchFilters,
) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, domain.ErrInvalidInput
	}
	if topK <= 0 {
		topK = 20
	}

	ftsQuery := toFTSQuery(query)
	sqlQuery := SearchMemoriesSQL
	args := make([]any, 0, topK+8)
	if ftsQuery == "" {
		sqlQuery = ListMemoriesRecentSQL
		args = append(args, tenantID)
	} else {
		args = append(args, tenantID, ftsQuery)
	}
	var filterArgs []any
	if ftsQuery == "" {
		sqlQuery, filterArgs = appendMemoryFilterClause(sqlQuery, "", filters)
	} else {
		sqlQuery, filterArgs = appendMemoryFilterClause(sqlQuery, "m", filters)
	}
	args = append(args, filterArgs...)
	args = append(args, topK)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()

	memories := make([]domain.Memory, 0, topK)
	for rows.Next() {
		m, err := scanMemory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan memory row: %w", err)
		}
		memories = append(memories, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory rows: %w", err)
	}

	return memories, nil
}

func (r *MemoryRepository) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, domain.ErrInvalidInput
	}
	if len(ids) == 0 {
		return []domain.Memory{}, nil
	}

	uniqueIDs := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return []domain.Memory{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(uniqueIDs)), ",")
	query := GetMemoriesByIDsBaseSQL + " AND id IN (" + placeholders + ")"

	args := make([]any, 0, len(uniqueIDs)+1)
	args = append(args, tenantID)
	for _, id := range uniqueIDs {
		args = append(args, id)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get memories by ids: %w", err)
	}
	defer rows.Close()

	memories := make([]domain.Memory, 0, len(uniqueIDs))
	for rows.Next() {
		m, err := scanMemory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan memory by ids: %w", err)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories by ids: %w", err)
	}

	// Preserve caller order.
	slices.SortFunc(memories, func(a, b domain.Memory) int {
		return cmpIndex(uniqueIDs, a.ID) - cmpIndex(uniqueIDs, b.ID)
	})

	return memories, nil
}

func (r *MemoryRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, CountMemoriesSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return count, nil
}

func (r *MemoryRepository) FindByCanonicalKey(
	ctx context.Context,
	tenantID, canonicalKey string,
) (*domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(canonicalKey) == "" {
		return nil, nil
	}

	row := r.db.QueryRowContext(ctx, FindMemoryByCanonicalKeySQL, tenantID, canonicalKey)
	memory, err := scanMemory(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find memory by canonical key: %w", err)
	}
	return &memory, nil
}

func (r *MemoryRepository) ListBySourceTurnHash(
	ctx context.Context,
	tenantID, sourceTurnHash string,
	limit int,
) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(sourceTurnHash) == "" {
		return []domain.Memory{}, nil
	}
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.QueryContext(ctx, ListMemoriesBySourceTurnHashSQL, tenantID, sourceTurnHash, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories by source turn hash: %w", err)
	}
	defer rows.Close()

	memories := make([]domain.Memory, 0, limit)
	for rows.Next() {
		m, err := scanMemory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan memory by source turn hash: %w", err)
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories by source turn hash: %w", err)
	}
	return memories, nil
}

func (r *MemoryRepository) Touch(ctx context.Context, tenantID string, ids []string) error {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ErrInvalidInput
	}
	if len(ids) == 0 {
		return nil
	}

	uniqueIDs := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(uniqueIDs)), ",")
	query := "UPDATE memories SET last_accessed_at = ?, last_recalled_at = ?, recall_count = recall_count + 1, updated_at = ? WHERE tenant_id = ? AND id IN (" + placeholders + ")"

	args := make([]any, 0, len(uniqueIDs)+4)
	args = append(args, now, now, now, tenantID)
	for _, id := range uniqueIDs {
		args = append(args, id)
	}

	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("touch memories: %w", err)
	}
	return nil
}

func cmpIndex(ids []string, id string) int {
	for i := range ids {
		if ids[i] == id {
			return i
		}
	}
	return len(ids) + 1
}

func prepareMemoryForStore(m *domain.Memory, now time.Time) error {
	if m == nil {
		return domain.ErrInvalidInput
	}
	if strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(m.Content) == "" {
		return domain.ErrInvalidInput
	}
	if m.ID == "" {
		m.ID = newID("mem")
	}
	if m.Tier == "" {
		m.Tier = domain.MemoryTierAuto
	}
	if m.CreatedBy == "" {
		m.CreatedBy = domain.MemoryCreatedByAuto
	}
	if m.Kind == "" {
		m.Kind = domain.MemoryKindRawTurn
	}
	if m.SourceFactIndex == 0 && strings.TrimSpace(m.CanonicalKey) == "" {
		m.SourceFactIndex = -1
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.LastAccessedAt.IsZero() {
		m.LastAccessedAt = now
	}
	if m.LastRecalledAt.IsZero() {
		m.LastRecalledAt = time.Unix(0, 0).UTC()
	}
	if len(m.Tags) == 0 {
		m.Tags = []string{}
	}
	if m.RecallCount < 0 {
		m.RecallCount = 0
	}
	return nil
}

func insertMemoryTx(ctx context.Context, tx *sql.Tx, m domain.Memory, tagsJSON string) error {
	if _, err := tx.ExecContext(
		ctx,
		InsertMemorySQL,
		m.ID,
		m.TenantID,
		m.Content,
		m.QueryViewText,
		string(m.Tier),
		tagsJSON,
		m.Source,
		string(m.CreatedBy),
		string(m.Kind),
		m.CanonicalKey,
		m.SourceTurnHash,
		m.SourceFactIndex,
		m.Extractor,
		m.ExtractorVersion,
		m.Importance,
		m.RecallCount,
		m.CreatedAt.Format(time.RFC3339Nano),
		m.UpdatedAt.Format(time.RFC3339Nano),
		m.LastAccessedAt.Format(time.RFC3339Nano),
		m.LastRecalledAt.Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, InsertMemoryFTSSQL, buildIndexedMemoryText(m), m.TenantID, m.ID); err != nil {
		return fmt.Errorf("insert memory fts row: %w", err)
	}
	if err := upsertMemoryIndexJobTx(
		ctx,
		tx,
		m.TenantID,
		m.ID,
		domain.MemoryIndexOperationUpsert,
		domain.MemoryIndexStatePending,
		"",
	); err != nil {
		return fmt.Errorf("queue upsert index job: %w", err)
	}
	return nil
}

func buildIndexedMemoryText(m domain.Memory) string {
	content := strings.Join(strings.Fields(strings.TrimSpace(m.Content)), " ")
	contentClean := cleanIndexedContentText(content)
	queryView := strings.Join(strings.Fields(strings.TrimSpace(m.QueryViewText)), " ")
	speakers := extractIndexedSpeakerTokens(content)

	parts := make([]string, 0, 4)
	switch {
	case contentClean != "":
		parts = append(parts, contentClean)
	case content != "":
		parts = append(parts, content)
	}
	if speakers != "" {
		parts = append(parts, speakers)
	}
	if queryView != "" {
		// Repeat query-view text once to approximate field weighting in a single FTS column.
		parts = append(parts, queryView, queryView)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func cleanIndexedContentText(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = indexedTagPattern.ReplaceAllString(line, " ")
		line = indexedSpeakerPrefix.ReplaceAllString(line, "")
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func extractIndexedSpeakerTokens(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	add := func(raw string) {
		tokens := ftsTokenPattern.FindAllString(strings.ToLower(strings.TrimSpace(raw)), -1)
		if len(tokens) == 0 {
			return
		}
		normalized := strings.Join(tokens, " ")
		if len(normalized) < 2 {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	for _, match := range indexedSpeakerTag.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, line := range strings.Split(content, "\n") {
		match := indexedSpeakerCapture.FindStringSubmatch(line)
		if len(match) > 1 {
			add(match[1])
		}
	}
	return strings.Join(out, " ")
}

func scanMemory(scan func(dest ...any) error) (domain.Memory, error) {
	var (
		m               domain.Memory
		tier            string
		tagsJSON        string
		createdBy       string
		kind            string
		sourceFactIndex int
		importance      float64
		recallCount     int
		createdAtRaw    string
		updatedAtRaw    string
		accessedAtRaw   string
		recalledAtRaw   string
	)
	if err := scan(
		&m.ID,
		&m.TenantID,
		&m.Content,
		&m.QueryViewText,
		&tier,
		&tagsJSON,
		&m.Source,
		&createdBy,
		&kind,
		&m.CanonicalKey,
		&m.SourceTurnHash,
		&sourceFactIndex,
		&m.Extractor,
		&m.ExtractorVersion,
		&importance,
		&recallCount,
		&createdAtRaw,
		&updatedAtRaw,
		&accessedAtRaw,
		&recalledAtRaw,
	); err != nil {
		return domain.Memory{}, err
	}

	m.Tier = domain.MemoryTier(tier)
	m.CreatedBy = domain.MemoryCreatedBy(createdBy)
	m.Kind = domain.MemoryKind(kind)
	m.SourceFactIndex = sourceFactIndex
	m.Importance = importance
	m.RecallCount = recallCount
	if tagsJSON == "" {
		m.Tags = []string{}
	} else if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
		return domain.Memory{}, fmt.Errorf("unmarshal memory tags: %w", err)
	}

	var err error
	m.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("parse created_at: %w", err)
	}
	m.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("parse updated_at: %w", err)
	}
	m.LastAccessedAt, err = time.Parse(time.RFC3339Nano, accessedAtRaw)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("parse last_accessed_at: %w", err)
	}
	m.LastRecalledAt, err = time.Parse(time.RFC3339Nano, recalledAtRaw)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("parse last_recalled_at: %w", err)
	}
	return m, nil
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		now := time.Now().UnixNano()
		for i := range raw {
			raw[i] = byte(now >> (i * 8))
		}
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func toFTSQuery(raw string) string {
	rawTokens := ftsTokenPattern.FindAllString(strings.ToLower(strings.TrimSpace(raw)), -1)
	if len(rawTokens) == 0 {
		return ""
	}
	tokens := make([]string, 0, len(rawTokens))
	seen := make(map[string]struct{}, len(rawTokens))
	fallbackOnly := false
	for _, token := range rawTokens {
		if len(token) < 2 {
			continue
		}
		if _, blocked := ftsQueryStopwordPattern[token]; blocked {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
		if len(tokens) >= maxFTSQueryTokens {
			break
		}
	}
	if len(tokens) == 0 {
		fallbackOnly = true
		for _, token := range rawTokens {
			if len(token) < 2 {
				continue
			}
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			tokens = append(tokens, token)
			if len(tokens) >= 4 {
				break
			}
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	if fallbackOnly {
		return strings.Join(tokens, " OR ")
	}
	if len(tokens) == 1 {
		return tokens[0]
	}
	// Combine strict conjunctive match with broad disjunctive fallback.
	strictQuery := strings.Join(tokens, " ")
	broadQuery := strings.Join(tokens, " OR ")
	return "(" + strictQuery + ") OR (" + broadQuery + ")"
}

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
	defer tx.Rollback()

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
	defer tx.Rollback()

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
	defer tx.Rollback()

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
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, ListMemoryPostprocessJobIDsForClaimSQL, nowRaw, nowRaw, limit)
	if err != nil {
		return nil, fmt.Errorf("select claimable postprocess jobs: %w", err)
	}
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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

func appendMemoryFilterClause(
	baseSQL string,
	alias string,
	filters domain.MemorySearchFilters,
) (string, []any) {
	args := make([]any, 0, len(filters.Tiers)+len(filters.Kinds))
	clauses := make([]string, 0, 2)
	column := func(name string) string {
		if strings.TrimSpace(alias) == "" {
			return name
		}
		return alias + "." + name
	}

	kinds := normalizeKindsForFilter(filters.Kinds)
	if len(kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
		clauses = append(clauses, column("kind")+" IN ("+placeholders+")")
		for _, kind := range kinds {
			args = append(args, string(kind))
		}
	}
	tiers := normalizeTiersForFilter(filters.Tiers)
	if len(tiers) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tiers)), ",")
		clauses = append(clauses, column("tier")+" IN ("+placeholders+")")
		for _, tier := range tiers {
			args = append(args, string(tier))
		}
	}

	if len(clauses) == 0 {
		return baseSQL, args
	}
	filterClause := "\n  AND " + strings.Join(clauses, "\n  AND ")
	if strings.Contains(baseSQL, "\nORDER BY") {
		return strings.Replace(baseSQL, "\nORDER BY", filterClause+"\nORDER BY", 1), args
	}
	if strings.Contains(baseSQL, " ORDER BY") {
		return strings.Replace(baseSQL, " ORDER BY", filterClause+" ORDER BY", 1), args
	}
	return baseSQL + filterClause, args
}

func normalizeKindsForFilter(kinds []domain.MemoryKind) []domain.MemoryKind {
	if len(kinds) == 0 {
		return []domain.MemoryKind{}
	}
	seen := make(map[domain.MemoryKind]struct{}, len(kinds))
	out := make([]domain.MemoryKind, 0, len(kinds))
	for _, kind := range kinds {
		switch kind {
		case domain.MemoryKindRawTurn, domain.MemoryKindObservation, domain.MemoryKindSummary, domain.MemoryKindEvent:
		default:
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	return out
}

func normalizeTiersForFilter(tiers []domain.MemoryTier) []domain.MemoryTier {
	if len(tiers) == 0 {
		return []domain.MemoryTier{}
	}
	seen := make(map[domain.MemoryTier]struct{}, len(tiers))
	out := make([]domain.MemoryTier, 0, len(tiers))
	for _, tier := range tiers {
		switch tier {
		case domain.MemoryTierWorking, domain.MemoryTierEpisodic, domain.MemoryTierSemantic:
		default:
			continue
		}
		if _, ok := seen[tier]; ok {
			continue
		}
		seen[tier] = struct{}{}
		out = append(out, tier)
	}
	return out
}

