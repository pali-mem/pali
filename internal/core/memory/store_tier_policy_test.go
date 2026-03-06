package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
)

type capturingMemoryRepoStub struct {
	lastStored domain.Memory
}

func (r *capturingMemoryRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	r.lastStored = m
	return m, nil
}

func (*capturingMemoryRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}

func (*capturingMemoryRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}

func (*capturingMemoryRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}

func (*capturingMemoryRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

func TestStore_AutoTierDefaultsToEpisodic(t *testing.T) {
	repo := &capturingMemoryRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "met teammate for coffee today",
		Tier:     domain.MemoryTierAuto,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryTierEpisodic, stored.Tier)
	require.Equal(t, domain.MemoryTierEpisodic, repo.lastStored.Tier)
}

func TestStore_AutoTierPromotesUserPreferenceToSemantic(t *testing.T) {
	repo := &capturingMemoryRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "User prefers concise responses.",
		Tier:     domain.MemoryTierAuto,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryTierSemantic, stored.Tier)
	require.Equal(t, domain.MemoryTierSemantic, repo.lastStored.Tier)
}

func TestStore_ExplicitTierIsPreserved(t *testing.T) {
	repo := &capturingMemoryRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "short-term event",
		Tier:     domain.MemoryTierWorking,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryTierWorking, stored.Tier)
	require.Equal(t, domain.MemoryTierWorking, repo.lastStored.Tier)
}
