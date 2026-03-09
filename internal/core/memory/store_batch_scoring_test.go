package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

type batchScoringRepoStub struct {
	stored []domain.Memory
}

func (r *batchScoringRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	r.stored = append(r.stored, m)
	return m, nil
}

func (*batchScoringRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}

func (*batchScoringRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}

func (*batchScoringRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}

func (*batchScoringRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

type batchOnlyScorer struct {
	scoreCalls int
	batchCalls int
	lastBatch  []string
}

func (s *batchOnlyScorer) Score(ctx context.Context, text string) (float64, error) {
	s.scoreCalls++
	return 0, fmt.Errorf("single score path should not be used")
}

func (s *batchOnlyScorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
	s.batchCalls++
	s.lastBatch = append([]string{}, texts...)
	out := make([]float64, 0, len(texts))
	for i := range texts {
		out = append(out, float64(i+1)/10.0)
	}
	return out, nil
}

func TestStoreBatchUsesBatchImportanceScorer(t *testing.T) {
	repo := &batchScoringRepoStub{}
	scorer := &batchOnlyScorer{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorer,
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "alpha"},
		{TenantID: "tenant_1", Content: "beta"},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, 1, scorer.batchCalls)
	require.Equal(t, 0, scorer.scoreCalls)
	require.Equal(t, []string{"alpha", "beta"}, scorer.lastBatch)
	require.Equal(t, 0.1, stored[0].Importance)
	require.Equal(t, 0.2, stored[1].Importance)
}
