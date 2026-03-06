package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/vein05/pali/internal/domain"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" || len(embedding) == 0 {
		return domain.ErrInvalidInput
	}

	raw, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		UpsertEmbeddingSQL,
		tenantID,
		memoryID,
		string(raw),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}

	return nil
}

func (s *Store) UpsertBatch(ctx context.Context, upserts []domain.VectorUpsert) error {
	if len(upserts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding upsert batch transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range upserts {
		entry := upserts[i]
		if strings.TrimSpace(entry.TenantID) == "" || strings.TrimSpace(entry.MemoryID) == "" || len(entry.Embedding) == 0 {
			return domain.ErrInvalidInput
		}
		raw, err := json.Marshal(entry.Embedding)
		if err != nil {
			return fmt.Errorf("marshal embedding[%d]: %w", i, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			UpsertEmbeddingSQL,
			entry.TenantID,
			entry.MemoryID,
			string(raw),
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

func (s *Store) Delete(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.ErrInvalidInput
	}
	if _, err := s.db.ExecContext(ctx, DeleteEmbeddingSQL, tenantID, memoryID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	return nil
}

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
	defer rows.Close()

	candidates := make([]domain.VectorstoreCandidate, 0, topK)
	for rows.Next() {
		var (
			memoryID string
			raw      string
		)
		if err := rows.Scan(&memoryID, &raw); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}

		var vec []float32
		if err := json.Unmarshal([]byte(raw), &vec); err != nil {
			return nil, fmt.Errorf("unmarshal embedding row: %w", err)
		}
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
