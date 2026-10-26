package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

// Store persists embeddings in SQLite and performs in-process cosine search.
type Store struct {
	db *sql.DB
}

// NewStore builds a SQLite-backed vector store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Upsert stores a single embedding for a memory.
func (s *Store) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" || len(embedding) == 0 {
		return domain.ErrInvalidInput
	}

	if _, err := s.db.ExecContext(
		ctx,
		UpsertEmbeddingSQL,
		tenantID,
		memoryID,
		encodeEmbedding(embedding),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}

	return nil
}

// UpsertBatch stores embeddings for multiple memories in one transaction.
func (s *Store) UpsertBatch(ctx context.Context, upserts []domain.VectorUpsert) error {
	if len(upserts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding upsert batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range upserts {
		entry := upserts[i]
		if strings.TrimSpace(entry.TenantID) == "" || strings.TrimSpace(entry.MemoryID) == "" || len(entry.Embedding) == 0 {
			return domain.ErrInvalidInput
		}
		if _, err := tx.ExecContext(
			ctx,
			UpsertEmbeddingSQL,
			entry.TenantID,
			entry.MemoryID,
			encodeEmbedding(entry.Embedding),
			now,
		); err != nil {
			return fmt.Errorf("upsert embedding[%d]: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit embedding upsert batch transaction: %w", err)
	}
	return nil
}

// Delete removes an embedding for the given tenant and memory.
func (s *Store) Delete(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.ErrInvalidInput
	}
	if _, err := s.db.ExecContext(ctx, DeleteEmbeddingSQL, tenantID, memoryID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	return nil
}

// Search returns the most similar embeddings for a tenant.
func (s *Store) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	if strings.TrimSpace(tenantID) == "" || len(embedding) == 0 {
		return nil, domain.ErrInvalidInput
	}
	if topK <= 0 {
		topK = 10
	}

	rows, err := s.db.QueryContext(ctx, ListEmbeddingsByTenantSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	candidates := make([]domain.VectorstoreCandidate, 0, topK)
	var buf []float32
	for rows.Next() {
		var (
			memoryID string
			blob     []byte
			legacy   string
		)
		if err := rows.Scan(&memoryID, &blob, &legacy); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}

		vec, err := decodeStoredEmbedding(blob, legacy, buf)
		if err != nil {
			return nil, err
		}
		buf = vec
		if len(vec) == 0 {
			continue
		}

		similarity := cosineSimilarity(embedding, vec)
		candidates = append(candidates, domain.VectorstoreCandidate{
			MemoryID:   memoryID,
			Similarity: similarity,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embeddings: %w", err)
	}

	slices.SortFunc(candidates, func(a, b domain.VectorstoreCandidate) int {
		switch {
		case a.Similarity > b.Similarity:
			return -1
		case a.Similarity < b.Similarity:
			return 1
		default:
			return 0
		}
	})

	if len(candidates) > topK {
		return candidates[:topK], nil
	}
	return candidates, nil
}

// MigrateLegacyEmbeddings converts JSON embeddings written before the blob
// column existed. It is idempotent and cheap once nothing is left to convert.
func (s *Store) MigrateLegacyEmbeddings(ctx context.Context) error {
	const batchSize = 500
	for {
		rows, err := s.db.QueryContext(ctx, ListLegacyEmbeddingsSQL, batchSize)
		if err != nil {
			return fmt.Errorf("list legacy embeddings: %w", err)
		}
		type legacyRow struct {
			tenantID, memoryID string
			blob               []byte
		}
		batch := make([]legacyRow, 0, batchSize)
		for rows.Next() {
			var row legacyRow
			var raw string
			if err := rows.Scan(&row.tenantID, &row.memoryID, &raw); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan legacy embedding: %w", err)
			}
			var vec []float32
			if err := json.Unmarshal([]byte(raw), &vec); err != nil {
				_ = rows.Close()
				return fmt.Errorf("unmarshal legacy embedding %s: %w", row.memoryID, err)
			}
			row.blob = encodeEmbedding(vec)
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate legacy embeddings: %w", err)
		}
		_ = rows.Close()
		if len(batch) == 0 {
			return nil
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin legacy embedding conversion: %w", err)
		}
		for _, row := range batch {
			if _, err := tx.ExecContext(ctx, ConvertLegacyEmbeddingSQL, row.blob, row.tenantID, row.memoryID); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("convert legacy embedding %s: %w", row.memoryID, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit legacy embedding conversion: %w", err)
		}
		if len(batch) < batchSize {
			return nil
		}
	}
}

func encodeEmbedding(embedding []float32) []byte {
	out := make([]byte, 4*len(embedding))
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
	}
	return out
}

// decodeStoredEmbedding decodes a blob, or the legacy JSON when the blob is
// NULL, reusing buf's storage when it is large enough.
func decodeStoredEmbedding(blob []byte, legacy string, buf []float32) ([]float32, error) {
	if blob == nil {
		var vec []float32
		if err := json.Unmarshal([]byte(legacy), &vec); err != nil {
			return nil, fmt.Errorf("unmarshal embedding row: %w", err)
		}
		return vec, nil
	}
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("embedding blob has %d bytes, not a multiple of 4", len(blob))
	}
	n := len(blob) / 4
	if cap(buf) < n {
		buf = make([]float32, n)
	}
	buf = buf[:n]
	for i := range buf {
		buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[4*i:]))
	}
	return buf, nil
}

func cosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}

	var dot float64
	var magA float64
	var magB float64
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		magA += av * av
		magB += bv * bv
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
