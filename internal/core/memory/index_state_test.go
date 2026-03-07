package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/pali-mem/pali/internal/domain"
)

type indexStateTransition struct {
	tenantID  string
	memoryIDs []string
	op        domain.MemoryIndexOperation
	state     domain.MemoryIndexState
	lastError string
}

type indexStateRepoStub struct {
	stored      map[string]domain.Memory
	order       []string
	nextID      int
	deleteErr   error
	transitions []indexStateTransition
}

func newIndexStateRepoStub() *indexStateRepoStub {
	return &indexStateRepoStub{
		stored: make(map[string]domain.Memory),
		nextID: 1,
	}
}

func (r *indexStateRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	if strings.TrimSpace(m.ID) == "" {
		m.ID = fmt.Sprintf("mem_%d", r.nextID)
		r.nextID++
	}
	r.stored[m.ID] = m
	r.order = append(r.order, m.ID)
	return m, nil
}

func (r *indexStateRepoStub) StoreBatch(ctx context.Context, memories []domain.Memory) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(memories))
	for _, memory := range memories {
		stored, err := r.Store(ctx, memory)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (r *indexStateRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.stored, memoryID)
	return nil
}

func (r *indexStateRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}

func (r *indexStateRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(ids))
	for _, id := range ids {
		if memory, ok := r.stored[id]; ok {
			out = append(out, memory)
		}
	}
	return out, nil
}

func (r *indexStateRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

func (r *indexStateRepoStub) MarkIndexState(
	ctx context.Context,
	tenantID string,
	memoryIDs []string,
	op domain.MemoryIndexOperation,
	state domain.MemoryIndexState,
	lastError string,
) error {
	r.transitions = append(r.transitions, indexStateTransition{
		tenantID:  tenantID,
		memoryIDs: append([]string{}, memoryIDs...),
		op:        op,
		state:     state,
		lastError: lastError,
	})
	return nil
}

type indexStateVectorStub struct {
	upsertCalls int
	upsertErrAt int
	deleteErr   error
}

func (v *indexStateVectorStub) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	v.upsertCalls++
	if v.upsertErrAt > 0 && v.upsertCalls == v.upsertErrAt {
		return errors.New("vector upsert failed")
	}
	return nil
}

func (v *indexStateVectorStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	if v.deleteErr != nil {
		return v.deleteErr
	}
	return nil
}

func (v *indexStateVectorStub) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	return []domain.VectorstoreCandidate{}, nil
}

func TestStoreMarksIndexStateTransitions(t *testing.T) {
	repo := newIndexStateRepoStub()
	vector := &indexStateVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "alpha"},
		{TenantID: "tenant_1", Content: "beta"},
	})
	require.NoError(t, err)
	require.Len(t, repo.transitions, 2)

	require.Equal(t, domain.MemoryIndexOperationUpsert, repo.transitions[0].op)
	require.Equal(t, domain.MemoryIndexStatePending, repo.transitions[0].state)
	require.Equal(t, domain.MemoryIndexOperationUpsert, repo.transitions[1].op)
	require.Equal(t, domain.MemoryIndexStateIndexed, repo.transitions[1].state)
	require.ElementsMatch(t, repo.transitions[0].memoryIDs, repo.transitions[1].memoryIDs)
	require.Len(t, repo.transitions[0].memoryIDs, 2)
}

func TestStoreMarksIndexStateFailedOnVectorFailure(t *testing.T) {
	repo := newIndexStateRepoStub()
	vector := &indexStateVectorStub{upsertErrAt: 1}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "alpha"},
		{TenantID: "tenant_1", Content: "beta"},
	})
	require.Error(t, err)
	require.Len(t, repo.transitions, 2)
	require.Equal(t, domain.MemoryIndexStatePending, repo.transitions[0].state)
	require.Equal(t, domain.MemoryIndexStateFailed, repo.transitions[1].state)
	require.Equal(t, domain.MemoryIndexOperationUpsert, repo.transitions[1].op)
	require.NotEmpty(t, repo.transitions[1].lastError)
}

func TestDeleteMarksIndexStateTombstoned(t *testing.T) {
	repo := newIndexStateRepoStub()
	_, _ = repo.Store(context.Background(), domain.Memory{
		ID:       "mem_1",
		TenantID: "tenant_1",
		Content:  "alpha",
	})
	vector := &indexStateVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
	)

	err := svc.Delete(context.Background(), "tenant_1", "mem_1")
	require.NoError(t, err)
	require.Len(t, repo.transitions, 2)
	require.Equal(t, domain.MemoryIndexOperationDelete, repo.transitions[0].op)
	require.Equal(t, domain.MemoryIndexStatePending, repo.transitions[0].state)
	require.Equal(t, domain.MemoryIndexOperationDelete, repo.transitions[1].op)
	require.Equal(t, domain.MemoryIndexStateTombstoned, repo.transitions[1].state)
}

func TestDeleteMarksIndexStateFailedOnVectorFailure(t *testing.T) {
	repo := newIndexStateRepoStub()
	_, _ = repo.Store(context.Background(), domain.Memory{
		ID:       "mem_1",
		TenantID: "tenant_1",
		Content:  "alpha",
	})
	vector := &indexStateVectorStub{deleteErr: errors.New("vector delete failed")}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
	)

	err := svc.Delete(context.Background(), "tenant_1", "mem_1")
	require.Error(t, err)
	require.Len(t, repo.transitions, 2)
	require.Equal(t, domain.MemoryIndexOperationDelete, repo.transitions[0].op)
	require.Equal(t, domain.MemoryIndexStatePending, repo.transitions[0].state)
	require.Equal(t, domain.MemoryIndexOperationDelete, repo.transitions[1].op)
	require.Equal(t, domain.MemoryIndexStateFailed, repo.transitions[1].state)
	require.NotEmpty(t, repo.transitions[1].lastError)
}
