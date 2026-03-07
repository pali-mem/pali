package qdrant

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
)

type fakePoint struct {
	ID       string
	TenantID string
	MemoryID string
	Vector   []float32
}

type fakeQdrant struct {
	exists bool
	size   int
	points map[string]fakePoint
}

func newFakeQdrant() *fakeQdrant {
	return &fakeQdrant{points: map[string]fakePoint{}}
}

func (f *fakeQdrant) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/collections/"):
		f.handleGetCollection(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/collections/") && !strings.Contains(r.URL.Path, "/points"):
		f.handleCreateCollection(w, r)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/points"):
		f.handleUpsert(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/points/search"):
		f.handleSearch(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/points/delete"):
		f.handleDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeQdrant) handleGetCollection(w http.ResponseWriter, _ *http.Request) {
	if !f.exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"result": map[string]any{
			"config": map[string]any{
				"params": map[string]any{
					"vectors": map[string]any{
						"size":     f.size,
						"distance": "Cosine",
					},
				},
			},
		},
		"status": "ok",
	})
}

func (f *fakeQdrant) handleCreateCollection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vectors struct {
			Size int `json:"size"`
		} `json:"vectors"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.exists = true
	f.size = req.Vectors.Size
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (f *fakeQdrant) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Points []struct {
			ID      any            `json:"id"`
			Vector  []float32      `json:"vector"`
			Payload map[string]any `json:"payload"`
		} `json:"points"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	for _, p := range req.Points {
		id := fmt.Sprint(p.ID)
		tenantID, _ := p.Payload["tenant_id"].(string)
		memoryID, _ := p.Payload["memory_id"].(string)
		f.points[id] = fakePoint{
			ID:       id,
			TenantID: tenantID,
			MemoryID: memoryID,
			Vector:   p.Vector,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (f *fakeQdrant) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vector []float32 `json:"vector"`
		Limit  int       `json:"limit"`
		Filter struct {
			Must []struct {
				Key   string `json:"key"`
				Match struct {
					Value string `json:"value"`
				} `json:"match"`
			} `json:"must"`
		} `json:"filter"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	tenantID := ""
	for _, must := range req.Filter.Must {
		if must.Key == "tenant_id" {
			tenantID = must.Match.Value
		}
	}

	type searchResult struct {
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	}
	results := make([]searchResult, 0, len(f.points))
	for _, point := range f.points {
		if point.TenantID != tenantID {
			continue
		}
		results = append(results, searchResult{
			Score: cosine(req.Vector, point.Vector),
			Payload: map[string]any{
				"tenant_id": point.TenantID,
				"memory_id": point.MemoryID,
			},
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": results, "status": "ok"})
}

func (f *fakeQdrant) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filter struct {
			Must []struct {
				Key   string `json:"key"`
				Match struct {
					Value string `json:"value"`
				} `json:"match"`
			} `json:"must"`
		} `json:"filter"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	tenantID := ""
	memoryID := ""
	for _, must := range req.Filter.Must {
		switch must.Key {
		case "tenant_id":
			tenantID = must.Match.Value
		case "memory_id":
			memoryID = must.Match.Value
		}
	}

	for id, point := range f.points {
		if point.TenantID == tenantID && point.MemoryID == memoryID {
			delete(f.points, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func cosine(a, b []float32) float64 {
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

func TestStoreUpsertSearchDelete(t *testing.T) {
	fake := newFakeQdrant()
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()

	client, err := NewClient(server.URL, "", "test_collection", time.Second)
	require.NoError(t, err)
	store := NewStore(client)

	ctx := context.Background()
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m1", []float32{1, 0}))
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m2", []float32{0, 1}))
	require.NoError(t, store.Upsert(ctx, "tenant_2", "other", []float32{1, 0}))

	candidates, err := store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "m1", candidates[0].MemoryID)
	require.Equal(t, "m2", candidates[1].MemoryID)

	require.NoError(t, store.Delete(ctx, "tenant_1", "m1"))
	candidates, err = store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "m2", candidates[0].MemoryID)
}

func TestStoreVectorSizeMismatch(t *testing.T) {
	fake := newFakeQdrant()
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()

	client, err := NewClient(server.URL, "", "test_collection", time.Second)
	require.NoError(t, err)
	store := NewStore(client)

	ctx := context.Background()
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m1", []float32{1, 0}))

	_, err = store.Search(ctx, "tenant_1", []float32{1, 0, 0}, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vector size mismatch")
}

func TestStoreUpsertBatch(t *testing.T) {
	fake := newFakeQdrant()
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()

	client, err := NewClient(server.URL, "", "test_collection", time.Second)
	require.NoError(t, err)
	store := NewStore(client)

	ctx := context.Background()
	upserts := []domain.VectorUpsert{
		{TenantID: "tenant_1", MemoryID: "m1", Embedding: []float32{1, 0}},
		{TenantID: "tenant_1", MemoryID: "m2", Embedding: []float32{0, 1}},
		{TenantID: "tenant_2", MemoryID: "other", Embedding: []float32{1, 0}},
	}
	require.NoError(t, store.UpsertBatch(ctx, upserts))

	// Verify tenant_1 results.
	candidates, err := store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "m1", candidates[0].MemoryID)
	require.Equal(t, "m2", candidates[1].MemoryID)

	// tenant_2 is isolated.
	candidates, err = store.Search(ctx, "tenant_2", []float32{1, 0}, 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "other", candidates[0].MemoryID)

	// Empty batch is a no-op.
	require.NoError(t, store.UpsertBatch(ctx, nil))
}

func TestStoreUpsertBatchValidation(t *testing.T) {
	fake := newFakeQdrant()
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()

	client, err := NewClient(server.URL, "", "test_collection", time.Second)
	require.NoError(t, err)
	store := NewStore(client)

	ctx := context.Background()

	// Missing tenant.
	err = store.UpsertBatch(ctx, []domain.VectorUpsert{{TenantID: "", MemoryID: "m1", Embedding: []float32{1, 0}}})
	require.Error(t, err)

	// Missing memory ID.
	err = store.UpsertBatch(ctx, []domain.VectorUpsert{{TenantID: "t1", MemoryID: "", Embedding: []float32{1, 0}}})
	require.Error(t, err)

	// Empty embedding.
	err = store.UpsertBatch(ctx, []domain.VectorUpsert{{TenantID: "t1", MemoryID: "m1", Embedding: nil}})
	require.Error(t, err)
}
