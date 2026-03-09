package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/domain"
)

type TenantRepository struct {
	db *sql.DB
}

func NewTenantRepository(db *sql.DB) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) Create(ctx context.Context, t domain.Tenant) (domain.Tenant, error) {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Name) == "" {
		return domain.Tenant{}, domain.ErrInvalidInput
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(ctx, InsertTenantSQL, t.ID, t.Name, t.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("insert tenant: %w", err)
	}
	return t, nil
}

func (r *TenantRepository) Exists(ctx context.Context, tenantID string) (bool, error) {
	if strings.TrimSpace(tenantID) == "" {
		return false, domain.ErrInvalidInput
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, TenantExistsSQL, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check tenant exists: %w", err)
	}
	return exists, nil
}

func (r *TenantRepository) MemoryCount(ctx context.Context, tenantID string) (int64, error) {
	if strings.TrimSpace(tenantID) == "" {
		return 0, domain.ErrInvalidInput
	}
	var count int64
	if err := r.db.QueryRowContext(ctx, CountTenantMemoriesSQL, tenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tenant memories: %w", err)
	}
	return count, nil
}

func (r *TenantRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, CountTenantsSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tenants: %w", err)
	}
	return count, nil
}

func (r *TenantRepository) List(ctx context.Context, limit int) ([]domain.Tenant, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx, ListTenantsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Tenant, 0, limit)
	for rows.Next() {
		var (
			t            domain.Tenant
			createdAtRaw string
		)
		if err := rows.Scan(&t.ID, &t.Name, &createdAtRaw); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		t.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse tenant created_at: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return out, nil
}

