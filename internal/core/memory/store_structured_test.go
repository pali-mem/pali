package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

type structuredRepoStub struct {
	stored []domain.Memory
}

func (r *structuredRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	if m.ID == "" {
		m.ID = fmt.Sprintf("mem_%d", len(r.stored)+1)
	}
	r.stored = append(r.stored, m)
	return m, nil
}

func (r *structuredRepoStub) StoreBatch(ctx context.Context, memories []domain.Memory) ([]domain.Memory, error) {
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

func (r *structuredRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	for i := range r.stored {
		if r.stored[i].TenantID == tenantID && r.stored[i].ID == memoryID {
			r.stored = append(r.stored[:i], r.stored[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *structuredRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, topK)
	for i := len(r.stored) - 1; i >= 0; i-- {
		if r.stored[i].TenantID != tenantID {
			continue
		}
		out = append(out, r.stored[i])
		if len(out) >= topK {
			break
		}
	}
	return out, nil
}

func (r *structuredRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(ids))
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
		for _, memory := range r.stored {
			if memory.ID == id && memory.TenantID == tenantID {
				out = append(out, memory)
				break
			}
		}
	}
	return out, nil
}

func (r *structuredRepoStub) FindByCanonicalKey(
	ctx context.Context,
	tenantID, canonicalKey string,
) (*domain.Memory, error) {
	for i := len(r.stored) - 1; i >= 0; i-- {
		memory := r.stored[i]
		if memory.TenantID != tenantID || memory.CanonicalKey != canonicalKey {
			continue
		}
		return &memory, nil
	}
	return nil, nil
}

func (*structuredRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

type structuredVectorStub struct {
	upserted   []string
	batchCalls int
	embeddings map[string][]float32
}

func (v *structuredVectorStub) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if v.embeddings == nil {
		v.embeddings = make(map[string][]float32, 32)
	}
	v.upserted = append(v.upserted, memoryID)
	v.embeddings[memoryID] = append([]float32{}, embedding...)
	return nil
}

func (v *structuredVectorStub) UpsertBatch(ctx context.Context, upserts []domain.VectorUpsert) error {
	v.batchCalls++
	for _, entry := range upserts {
		if err := v.Upsert(ctx, entry.TenantID, entry.MemoryID, entry.Embedding); err != nil {
			return err
		}
	}
	return nil
}

func (v *structuredVectorStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	delete(v.embeddings, memoryID)
	return nil
}

func (v *structuredVectorStub) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	candidates := make([]domain.VectorstoreCandidate, 0, len(v.embeddings))
	for memoryID, vec := range v.embeddings {
		candidates = append(candidates, domain.VectorstoreCandidate{
			MemoryID:   memoryID,
			Similarity: cosineSimilarityF32(embedding, vec),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Similarity > candidates[j].Similarity
	})
	if topK > 0 && len(candidates) > topK {
		return candidates[:topK], nil
	}
	return candidates, nil
}

type structuredEmbedderStub struct{}

func (structuredEmbedderStub) Embed(ctx context.Context, text string) ([]float32, error) {
	tokens := rankingTokenPattern.FindAllString(strings.ToLower(text), -1)
	vec := make([]float32, 32)
	if len(tokens) == 0 {
		vec[0] = 1
		return vec, nil
	}
	for _, token := range tokens {
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		idx := int(h.Sum32() % uint32(len(vec)))
		vec[idx] += 1
	}
	return vec, nil
}

func cosineSimilarityF32(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, magA, magB float64
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

func countKind(memories []domain.Memory, kind domain.MemoryKind) int {
	total := 0
	for _, memory := range memories {
		if memory.Kind == kind {
			total++
		}
	}
	return total
}

func TestStoreStructuredMemoryDualWrite(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:               true,
			DualWriteObservations: true,
			MaxObservations:       2,
		},
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID:  "tenant_1",
		Content:   "I prefer tea over coffee. I usually drink green tea after lunch. I avoid sugary drinks in the evening.",
		Tier:      domain.MemoryTierAuto,
		Tags:      []string{"preferences"},
		Source:    "user_session",
		CreatedBy: domain.MemoryCreatedByUser,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)

	require.Len(t, repo.stored, 3)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[1].Kind)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[2].Kind)
	require.Equal(t, domain.MemoryCreatedBySystem, repo.stored[1].CreatedBy)
	require.Contains(t, repo.stored[1].Tags, "observation")
	require.Contains(t, repo.stored[1].Tags, "parser")
	require.Equal(t, "user_session:parser", repo.stored[1].Source)
	require.NotEmpty(t, repo.stored[1].CanonicalKey)
	require.Len(t, vector.upserted, 3)
}

func TestStoreStructuredMemoryNoDerivedForSingleSentence(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:               true,
			DualWriteObservations: true,
			MaxObservations:       3,
		},
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "User prefers concise replies.",
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 2)
	require.Len(t, vector.upserted, 2)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[1].Kind)
	require.Equal(t, "parser", repo.stored[1].Source)
}

func TestStoreStructuredMemoryDualWriteEvent(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:         true,
			DualWriteEvents: true,
			MaxObservations: 2,
		},
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 8 May, 2023] Caroline: I joined a support group yesterday.",
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 2)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
	require.Equal(t, domain.MemoryKindEvent, repo.stored[1].Kind)
	require.Contains(t, repo.stored[1].Content, "On 8 May 2023,")
	require.Contains(t, repo.stored[1].Content, "Caroline")
	require.Contains(t, repo.stored[1].Tags, "event")
	require.Contains(t, repo.stored[1].Tags, "parser")
	require.Equal(t, "parser", repo.stored[1].Source)
}

func TestStoreStructuredMemoryDualWriteRepeatedTurnDedupesObservation(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:               true,
			DualWriteObservations: true,
			MaxObservations:       1,
		},
	)

	input := StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:8:30 am on 3 June, 2024] Taylor: I always choose tea over coffee.",
	}
	_, err := svc.Store(context.Background(), input)
	require.NoError(t, err)
	_, err = svc.Store(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, countKind(repo.stored, domain.MemoryKindObservation))
}

func TestStoreStructuredMemoryDualWriteRepeatedTurnDedupesEvent(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:         true,
			DualWriteEvents: true,
			MaxObservations: 2,
		},
	)

	input := StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:8:30 am on 3 June, 2024] Taylor: I started a new role today.",
	}
	_, err := svc.Store(context.Background(), input)
	require.NoError(t, err)
	_, err = svc.Store(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, countKind(repo.stored, domain.MemoryKindEvent))
}

func TestStructuredDualWriteUsesCanonicalParserPathAcrossRepeatedTurns(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:               true,
			DualWriteObservations: true,
			MaxObservations:       2,
		},
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:8:30 am on 3 June, 2024] Taylor: I always choose tea over coffee.",
	})
	require.NoError(t, err)
	_, err = svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:8:30 am on 3 June, 2024] Taylor: I always choose tea over coffee.",
	})
	require.NoError(t, err)

	require.Equal(t, 1, countKind(repo.stored, domain.MemoryKindObservation))
	for _, memory := range repo.stored {
		if memory.Kind == domain.MemoryKindObservation {
			require.Equal(t, "parser", memory.Source)
			require.NotEmpty(t, memory.CanonicalKey)
		}
	}
}
