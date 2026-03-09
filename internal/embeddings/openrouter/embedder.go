package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultTimeout = 10 * time.Second
)

var (
	openRouterMaxBatchInputs     = 50
	openRouterMaxParallelBatches = 4
)

type Embedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

type embeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

func NewEmbedder(baseURL, apiKey, model string, timeout time.Duration) (*Embedder, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter api key is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("openrouter embedding model is required")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Embedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.BatchEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return embeddings[0], nil
}

func (e *Embedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("openrouter embedder is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if len(texts) <= openRouterMaxBatchInputs {
		return e.embedBatch(ctx, texts)
	}

	chunks := chunkTexts(texts, openRouterMaxBatchInputs)
	out := make([][]float32, len(texts))

	parallel := openRouterMaxParallelBatches
	if parallel < 1 {
		parallel = 1
	}
	if parallel > len(chunks) {
		parallel = len(chunks)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, parallel)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for _, chunk := range chunks {
		chunk := chunk
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			vectors, err := e.embedBatch(ctx, chunk.Texts)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("openrouter batch embed failed at offset %d: %w", chunk.Start, err):
					cancel()
				default:
				}
				return
			}
			for i := range vectors {
				out[chunk.Start+i] = vectors[i]
			}
		}()
	}

	wg.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("openrouter embeddings response missing embedding for index %d", i)
		}
	}
	return out, nil
}

func (e *Embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := embeddingsRequest{
		Model: e.model,
		Input: texts,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openrouter embeddings request: %w", err)
	}

	respBody, err := e.do(ctx, http.MethodPost, "/embeddings", body)
	if err != nil {
		return nil, err
	}

	var parsed embeddingsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode openrouter embeddings response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openrouter embeddings count mismatch: got %d embeddings for %d inputs", len(parsed.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for i, item := range parsed.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("openrouter embeddings response has empty embedding at index %d", i)
		}
		idx := item.Index
		if idx < 0 || idx >= len(texts) {
			if len(parsed.Data) == len(texts) {
				idx = i
			} else {
				return nil, fmt.Errorf("openrouter embeddings response index out of range: %d", item.Index)
			}
		}
		out[idx] = append([]float32{}, item.Embedding...)
	}
	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("openrouter embeddings response missing embedding for index %d", i)
		}
	}
	return out, nil
}

type textChunk struct {
	Start int
	Texts []string
}

func chunkTexts(texts []string, chunkSize int) []textChunk {
	if chunkSize <= 0 || len(texts) <= chunkSize {
		return []textChunk{{Start: 0, Texts: texts}}
	}
	chunks := make([]textChunk, 0, (len(texts)+chunkSize-1)/chunkSize)
	for start := 0; start < len(texts); start += chunkSize {
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		chunks = append(chunks, textChunk{
			Start: start,
			Texts: texts[start:end],
		})
	}
	return chunks
}

func (e *Embedder) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	url := e.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request %s %s: %w", method, url, err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response %s %s: %w", method, url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		var apiErr apiErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			if strings.TrimSpace(apiErr.Error.Message) != "" {
				msg = strings.TrimSpace(apiErr.Error.Message)
			} else if strings.TrimSpace(apiErr.Message) != "" {
				msg = strings.TrimSpace(apiErr.Message)
			}
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("openrouter %s %s failed: %s", method, path, msg)
	}

	return respBody, nil
}
