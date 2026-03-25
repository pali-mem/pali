package pgvector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pali-mem/pali/internal/domain"
)

// Options configures the pgvector-backed vector store.
type Options struct {
	DSN          string
	Table        string
	AutoMigrate  bool
	MaxOpenConns int
	MaxIdleConns int
}

// Store persists vectors in PostgreSQL using the pgvector extension.
type Store struct {
	db          *sql.DB
	table       string
	autoMigrate bool

	mu        sync.Mutex
	ready     bool
	vectorDim int
}

// NewStore opens a Postgres connection and prepares a pgvector-backed store.
func NewStore(opts Options) (*Store, error) {
	dsn := strings.TrimSpace(opts.DSN)
	if dsn == "" {
		return nil, fmt.Errorf("pgvector dsn is required")
	}

	table := normalizeIdentifier(opts.Table)
	if table == "" {
		table = "pali_memories"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pgvector database: %w", err)
	}
	if opts.MaxOpenConns > 0 {
		db.SetMaxOpenConns(opts.MaxOpenConns)
	}
	if opts.MaxIdleConns > 0 {
		db.SetMaxIdleConns(opts.MaxIdleConns)
	}

	store := &Store{
		db:          db,
		table:       table,
		autoMigrate: opts.AutoMigrate,
	}
	return store, nil
}

// Close releases the underlying Postgres connection pool.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Upsert stores or updates a single embedding.
func (s *Store) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" || len(embedding) == 0 {
		return domain.ErrInvalidInput
	}
	if err := s.ensureReady(ctx, len(embedding)); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(
		ctx,
		s.query(upsertSQL),
		tenantID,
		memoryID,
		vectorLiteral(embedding),
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("upsert pgvector embedding: %w", err)
	}
	return nil
}

// UpsertBatch stores multiple embeddings in one transaction.
func (s *Store) UpsertBatch(ctx context.Context, upserts []domain.VectorUpsert) error {
	if len(upserts) == 0 {
		return nil
	}
	for i, upsert := range upserts {
		if strings.TrimSpace(upsert.TenantID) == "" || strings.TrimSpace(upsert.MemoryID) == "" || len(upsert.Embedding) == 0 {
			return fmt.Errorf("upsert[%d]: %w", i, domain.ErrInvalidInput)
		}
		if len(upsert.Embedding) != len(upserts[0].Embedding) {
			return fmt.Errorf("upsert[%d]: vector dimension mismatch: got=%d expected=%d", i, len(upsert.Embedding), len(upserts[0].Embedding))
		}
	}
	if err := s.ensureReady(ctx, len(upserts[0].Embedding)); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pgvector upsert batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	for i := range upserts {
		entry := upserts[i]
		if _, err := tx.ExecContext(ctx, s.query(upsertSQL), entry.TenantID, entry.MemoryID, vectorLiteral(entry.Embedding), now); err != nil {
			return fmt.Errorf("upsert pgvector embedding[%d]: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit pgvector upsert batch transaction: %w", err)
	}
	return nil
}

// Delete removes a stored embedding by tenant and memory id.
func (s *Store) Delete(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.ErrInvalidInput
	}
	if err := s.ensureReady(ctx, 0); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, s.query(deleteSQL), tenantID, memoryID); err != nil {
		return fmt.Errorf("delete pgvector embedding: %w", err)
	}
	return nil
}

// Search returns the closest tenant-scoped vector matches.
func (s *Store) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	if strings.TrimSpace(tenantID) == "" || len(embedding) == 0 {
		return nil, domain.ErrInvalidInput
	}
	if topK <= 0 {
		topK = 10
	}
	if err := s.ensureReady(ctx, len(embedding)); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, s.query(searchSQL), tenantID, vectorLiteral(embedding), topK)
	if err != nil {
		return nil, fmt.Errorf("search pgvector embeddings: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	candidates := make([]domain.VectorstoreCandidate, 0, topK)
	for rows.Next() {
		var candidate domain.VectorstoreCandidate
		if err := rows.Scan(&candidate.MemoryID, &candidate.Similarity); err != nil {
			return nil, fmt.Errorf("scan pgvector candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pgvector candidates: %w", err)
	}
	return candidates, nil
}

func (s *Store) ensureReady(ctx context.Context, expectedDim int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgvector store database is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		if s.autoMigrate {
			if _, err := s.db.ExecContext(ctx, createExtensionSQL); err != nil {
				return fmt.Errorf("ensure pgvector extension: %w", err)
			}
			if _, err := s.db.ExecContext(ctx, s.query(createTableSQL)); err != nil {
				return fmt.Errorf("ensure pgvector table: %w", err)
			}
		}

		dim, err := s.detectVectorDim(ctx)
		if err != nil {
			return err
		}
		s.vectorDim = dim
		s.ready = true
	}

	if expectedDim > 0 {
		if s.vectorDim == 0 {
			s.vectorDim = expectedDim
		}
		if s.vectorDim != expectedDim {
			return fmt.Errorf("pgvector dimension mismatch: existing=%d new=%d", s.vectorDim, expectedDim)
		}
	}
	return nil
}

func (s *Store) detectVectorDim(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, s.query(detectVectorDimSQL))
	var dim sql.NullInt64
	if err := row.Scan(&dim); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("detect pgvector dimension: %w", err)
	}
	if !dim.Valid {
		return 0, nil
	}
	return int(dim.Int64), nil
}

func (s *Store) query(template string) string {
	return fmt.Sprintf(template, s.table)
}

func vectorLiteral(embedding []float32) string {
	parts := make([]string, 0, len(embedding))
	for _, value := range embedding {
		parts = append(parts, strconv.FormatFloat(float64(value), 'f', -1, 32))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func normalizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return ""
	}
	return value
}
