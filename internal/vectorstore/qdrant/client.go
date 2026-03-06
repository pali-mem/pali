package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vein05/pali/internal/domain"
)

const (
	defaultBaseURL    = "http://127.0.0.1:6333"
	defaultCollection = "pali_memories"
	defaultTimeout    = 2 * time.Second
)

type Client struct {
	baseURL    string
	apiKey     string
	collection string
	httpClient *http.Client

	mu             sync.Mutex
	collectionSize int
}

func NewClient(baseURL, apiKey, collection string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid qdrant base url: %w", err)
	}

	collection = strings.TrimSpace(collection)
	if collection == "" {
		collection = defaultCollection
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		collection: collection,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" || len(embedding) == 0 {
		return domain.ErrInvalidInput
	}
	if err := c.ensureCollection(ctx, len(embedding)); err != nil {
		return err
	}

	payload := map[string]any{
		"points": []map[string]any{
			{
				"id":     stablePointID(tenantID, memoryID),
				"vector": embedding,
				"payload": map[string]any{
					"tenant_id": tenantID,
					"memory_id": memoryID,
				},
			},
		},
	}
	_, err := c.request(ctx, http.MethodPut, "/collections/"+c.collection+"/points?wait=true", payload, false)
	if err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.ErrInvalidInput
	}
	filter := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "tenant_id", "match": map[string]any{"value": tenantID}},
				{"key": "memory_id", "match": map[string]any{"value": memoryID}},
			},
		},
	}
	_, err := c.request(ctx, http.MethodPost, "/collections/"+c.collection+"/points/delete?wait=true", filter, true)
	if err != nil {
		return fmt.Errorf("qdrant delete: %w", err)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	if strings.TrimSpace(tenantID) == "" || len(embedding) == 0 {
		return nil, domain.ErrInvalidInput
	}
	if topK <= 0 {
		topK = 10
	}
	if err := c.ensureCollection(ctx, len(embedding)); err != nil {
		return nil, err
	}

	req := map[string]any{
		"vector":       embedding,
		"limit":        topK,
		"with_payload": true,
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "tenant_id", "match": map[string]any{"value": tenantID}},
			},
		},
	}

	body, err := c.request(ctx, http.MethodPost, "/collections/"+c.collection+"/points/search", req, false)
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	var resp struct {
		Result []struct {
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode qdrant search response: %w", err)
	}

	candidates := make([]domain.VectorstoreCandidate, 0, len(resp.Result))
	for _, item := range resp.Result {
		memoryID, _ := item.Payload["memory_id"].(string)
		if strings.TrimSpace(memoryID) == "" {
			continue
		}
		candidates = append(candidates, domain.VectorstoreCandidate{
			MemoryID:   memoryID,
			Similarity: item.Score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Similarity == candidates[j].Similarity {
			return candidates[i].MemoryID < candidates[j].MemoryID
		}
		return candidates[i].Similarity > candidates[j].Similarity
	})
	if len(candidates) > topK {
		return candidates[:topK], nil
	}
	return candidates, nil
}

func (c *Client) ensureCollection(ctx context.Context, vectorSize int) error {
	if vectorSize <= 0 {
		return domain.ErrInvalidInput
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.collectionSize > 0 {
		if c.collectionSize != vectorSize {
			return fmt.Errorf("qdrant collection %q vector size mismatch: existing=%d new=%d", c.collection, c.collectionSize, vectorSize)
		}
		return nil
	}

	body, err := c.request(ctx, http.MethodGet, "/collections/"+c.collection, nil, true)
	if err != nil {
		return err
	}

	if len(body) == 0 {
		createReq := map[string]any{
			"vectors": map[string]any{
				"size":     vectorSize,
				"distance": "Cosine",
			},
		}
		if _, err := c.request(ctx, http.MethodPut, "/collections/"+c.collection, createReq, false); err != nil {
			return fmt.Errorf("create qdrant collection %q: %w", c.collection, err)
		}
		c.collectionSize = vectorSize
		return nil
	}

	size, err := parseCollectionVectorSize(body)
	if err != nil {
		return err
	}
	if size != vectorSize {
		return fmt.Errorf("qdrant collection %q vector size mismatch: existing=%d new=%d", c.collection, size, vectorSize)
	}
	c.collectionSize = size
	return nil
}

func parseCollectionVectorSize(body []byte) (int, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, fmt.Errorf("decode qdrant collection response: %w", err)
	}

	result, _ := raw["result"].(map[string]any)
	config, _ := result["config"].(map[string]any)
	params, _ := config["params"].(map[string]any)
	vectors := params["vectors"]
	if vectors == nil {
		return 0, fmt.Errorf("qdrant collection response missing vectors config")
	}

	size := extractVectorSize(vectors)
	if size <= 0 {
		return 0, fmt.Errorf("qdrant collection response missing vector size")
	}
	return size, nil
}

func extractVectorSize(v any) int {
	m, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	if size, ok := asInt(m["size"]); ok {
		return size
	}
	for _, nested := range m {
		if nestedMap, ok := nested.(map[string]any); ok {
			if size, ok := asInt(nestedMap["size"]); ok {
				return size
			}
		}
	}
	return 0
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case int32:
		return int(x), true
	default:
		return 0, false
	}
}

func (c *Client) request(ctx context.Context, method, path string, payload any, allowNotFound bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound && allowNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant %s %s failed: status=%d body=%s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func stablePointID(tenantID, memoryID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(memoryID))
	return h.Sum64()
}
