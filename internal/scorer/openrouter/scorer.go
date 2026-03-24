package openrouter

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"encoding/json"
	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
)

// Scorer scores memories through an OpenRouter client.
type Scorer struct {
	client *Client
}

// NewScorer returns an OpenRouter-backed scorer.
func NewScorer(c *Client) *Scorer { return &Scorer{client: c} }

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)`)
var thinkBlockPattern = regexp.MustCompile(`(?is)<think>.*?</think>`)
var thinkTagPattern = regexp.MustCompile(`(?i)</?think>`)
var (
	openRouterMaxBatchScores      = 50
	openRouterMaxParallelScorings = 8
)

// Score returns an importance score for a single memory.
func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("openrouter scorer is not configured")
	}
	content := strings.TrimSpace(text)
	if content == "" {
		return 0, nil
	}

	prompt := coreprompts.Score(content)
	raw, err := s.client.Generate(ctx, prompt)
	if err != nil {
		return 0, err
	}

	score, err := parseScore(raw)
	if err != nil {
		return 0, fmt.Errorf("parse openrouter importance score: %w", err)
	}
	return score, nil
}

// BatchScore scores a batch of memories with bounded parallelism.
func (s *Scorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("openrouter scorer is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(texts) == 0 {
		return []float64{}, nil
	}

	out := make([]float64, len(texts))
	activeIdx := make([]int, 0, len(texts))
	activeTexts := make([]string, 0, len(texts))
	for i := range texts {
		if strings.TrimSpace(texts[i]) == "" {
			out[i] = 0
			continue
		}
		activeIdx = append(activeIdx, i)
		activeTexts = append(activeTexts, texts[i])
	}
	if len(activeTexts) == 0 {
		return out, nil
	}

	chunks := chunkScoringTexts(activeTexts, openRouterMaxBatchScores)
	parallel := openRouterMaxParallelScorings
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

			scores, err := s.scoreBatch(ctx, chunk.Texts)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("openrouter batch score failed at offset %d: %w", chunk.Start, err):
					cancel()
				default:
				}
				return
			}
			for i := range scores {
				globalIdx := activeIdx[chunk.Start+i]
				out[globalIdx] = clampScore(scores[i])
			}
		}()
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	return out, nil
}

func (s *Scorer) scoreBatch(ctx context.Context, texts []string) ([]float64, error) {
	if len(texts) == 0 {
		return []float64{}, nil
	}
	prompt := coreprompts.BatchScore(texts)
	raw, err := s.client.Generate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	scores, err := parseScoreList(raw, len(texts))
	if err != nil {
		return nil, fmt.Errorf("parse openrouter batch importance scores: %w", err)
	}
	return scores, nil
}

func parseScore(raw string) (float64, error) {
	trimmed := sanitizeModelOutput(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("empty scorer response")
	}

	matches := numberPattern.FindAllString(trimmed, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("no numeric score found in %q", trimmed)
	}
	match := matches[len(matches)-1]

	parsed, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, nil
	}
	if parsed > 1 {
		return 1, nil
	}
	return parsed, nil
}

func sanitizeModelOutput(raw string) string {
	out := strings.TrimSpace(raw)
	if out == "" {
		return ""
	}
	out = thinkBlockPattern.ReplaceAllString(out, " ")
	out = thinkTagPattern.ReplaceAllString(out, " ")
	out = strings.TrimSpace(out)
	return out
}

func parseScoreList(raw string, want int) ([]float64, error) {
	if want <= 0 {
		return []float64{}, nil
	}
	trimmed := sanitizeModelOutput(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty scorer response")
	}

	type scoreEnvelope struct {
		Scores []float64 `json:"scores"`
	}
	parseJSON := func(input string) ([]float64, bool) {
		var payload scoreEnvelope
		if err := json.Unmarshal([]byte(input), &payload); err == nil && len(payload.Scores) > 0 {
			out := make([]float64, len(payload.Scores))
			for i := range payload.Scores {
				out[i] = clampScore(payload.Scores[i])
			}
			return out, true
		}
		return nil, false
	}
	if scores, ok := parseJSON(trimmed); ok {
		if len(scores) != want {
			return nil, fmt.Errorf("score count mismatch: got %d want %d", len(scores), want)
		}
		return scores, nil
	}

	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			if scores, ok := parseJSON(trimmed[start : end+1]); ok {
				if len(scores) != want {
					return nil, fmt.Errorf("score count mismatch: got %d want %d", len(scores), want)
				}
				return scores, nil
			}
		}
	}

	matches := numberPattern.FindAllString(trimmed, -1)
	if len(matches) < want {
		return nil, fmt.Errorf("expected %d scores, found %d numbers in %q", want, len(matches), trimmed)
	}
	// Prefer trailing values to avoid accidental list-index prefixes.
	matches = matches[len(matches)-want:]
	out := make([]float64, 0, want)
	for _, match := range matches {
		v, err := strconv.ParseFloat(match, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, clampScore(v))
	}
	return out, nil
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type scoringChunk struct {
	Start int
	Texts []string
}

func chunkScoringTexts(texts []string, chunkSize int) []scoringChunk {
	if chunkSize <= 0 || len(texts) <= chunkSize {
		return []scoringChunk{{Start: 0, Texts: texts}}
	}
	chunks := make([]scoringChunk, 0, (len(texts)+chunkSize-1)/chunkSize)
	for start := 0; start < len(texts); start += chunkSize {
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}
		chunks = append(chunks, scoringChunk{
			Start: start,
			Texts: texts[start:end],
		})
	}
	return chunks
}
