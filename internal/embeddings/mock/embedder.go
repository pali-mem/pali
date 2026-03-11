package mock

import (
	"context"
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

const defaultDimension = 256

var tokenPattern = regexp.MustCompile(`[a-zA-Z0-9_]+`)

// Embedder is the pure-Go lexical provider implementation (legacy name: mock).
type Embedder struct {
	dim int
}

func NewEmbedder() *Embedder {
	return &Embedder{
		dim: defaultDimension,
	}
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	_ = ctx
	tokens := tokenize(text)
	vec := make([]float64, e.dim)
	if len(tokens) == 0 {
		return float64To32(vec), nil
	}

	tf := termFrequency(tokens)
	for token, count := range tf {
		idx := hashToken(token) % uint32(e.dim)
		// Keep lexical embeddings stateless so stored vectors and query vectors are
		// generated in the same space regardless of ingest order or prior queries.
		vec[idx] += 1 + math.Log(float64(count))
	}

	normalize(vec)
	return float64To32(vec), nil
}

func (e *Embedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func tokenize(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	return tokenPattern.FindAllString(text, -1)
}

func termFrequency(tokens []string) map[string]int {
	out := make(map[string]int, len(tokens))
	for _, token := range tokens {
		out[token]++
	}
	return out
}

func hashToken(token string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return h.Sum32()
}

func normalize(vec []float64) {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
}

func float64To32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i := range in {
		out[i] = float32(in[i])
	}
	return out
}
