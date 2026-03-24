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

// MemoryRepository stores and queries memories plus related async job state in SQLite.
type MemoryRepository struct {
	db *sql.DB
}

// NewMemoryRepository builds a SQLite-backed memory repository.
func NewMemoryRepository(db *sql.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// Store inserts a single memory record.
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
	defer rollbackTx(tx)

	if err := insertMemoryTx(ctx, tx, m, string(tagsJSON)); err != nil {
		return domain.Memory{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Memory{}, fmt.Errorf("commit store transaction: %w", err)
	}

	return m, nil
}

// StoreBatch inserts a batch of memory records in one transaction.
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
	defer rollbackTx(tx)

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

// Delete removes a memory for a tenant.
func (r *MemoryRepository) Delete(ctx context.Context, tenantID, memoryID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete transaction: %w", err)
	}
	defer rollbackTx(tx)

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

// Search performs lexical memory search.
func (r *MemoryRepository) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return r.SearchWithFilters(ctx, tenantID, query, topK, domain.MemorySearchFilters{})
}

// SearchWithFilters performs lexical memory search with tier and kind filters.
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
	defer closeRows(rows)

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

// GetByIDs returns memories by ID for a tenant.
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
	defer closeRows(rows)

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

// Count returns the total number of memories.
func (r *MemoryRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, CountMemoriesSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return count, nil
}

// FindByCanonicalKey returns the memory with the given canonical key.
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

// ListBySourceTurnHash returns memories tied to a source turn.
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
	defer closeRows(rows)

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

// Touch updates the access timestamps for the given memories.
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
	metadataJSON, err := marshalAnswerMetadata(m.AnswerMetadata)
	if err != nil {
		return fmt.Errorf("marshal memory metadata: %w", err)
	}
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
		metadataJSON,
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
		parts = append(parts, queryView)
	}
	if surfaceSpan := strings.Join(strings.Fields(strings.TrimSpace(m.AnswerMetadata.SurfaceSpan)), " "); surfaceSpan != "" {
		parts = append(parts, surfaceSpan)
	}
	if sourceSentence := strings.Join(strings.Fields(strings.TrimSpace(m.AnswerMetadata.SourceSentence)), " "); sourceSentence != "" && !strings.EqualFold(sourceSentence, contentClean) {
		parts = append(parts, sourceSentence)
	}
	if len(m.AnswerMetadata.SupportLines) > 0 {
		for _, line := range m.AnswerMetadata.SupportLines {
			line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
			if line == "" {
				continue
			}
			parts = append(parts, line)
		}
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
		metadataJSON    string
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
		&metadataJSON,
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
	if metadataJSON == "" {
		m.AnswerMetadata = domain.MemoryAnswerMetadata{}
	} else if err := json.Unmarshal([]byte(metadataJSON), &m.AnswerMetadata); err != nil {
		return domain.Memory{}, fmt.Errorf("unmarshal memory metadata: %w", err)
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

func marshalAnswerMetadata(metadata domain.MemoryAnswerMetadata) (string, error) {
	normalized := metadata
	if len(normalized.SupportMemoryIDs) == 0 {
		normalized.SupportMemoryIDs = []string{}
	}
	if len(normalized.SupportLines) == 0 {
		normalized.SupportLines = []string{}
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
