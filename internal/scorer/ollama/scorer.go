package ollama

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
)

// Scorer scores memories through an Ollama client.
type Scorer struct {
	client *Client
}

// NewScorer returns an Ollama-backed scorer.
func NewScorer(c *Client) *Scorer { return &Scorer{client: c} }

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)`)
var thinkBlockPattern = regexp.MustCompile(`(?is)<think>.*?</think>`)
var thinkTagPattern = regexp.MustCompile(`(?i)</?think>`)
var ollamaMaxParallelScores = 4

// Score returns an importance score for a single memory.
func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("ollama scorer is not configured")
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
		return 0, fmt.Errorf("parse ollama importance score: %w", err)
	}
	return score, nil
}

// BatchScore scores a batch of memories with bounded parallelism.
func (s *Scorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("ollama scorer is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(texts) == 0 {
		return []float64{}, nil
	}
	out := make([]float64, len(texts))

	parallel := ollamaMaxParallelScores
	if parallel < 1 {
		parallel = 1
	}
	if parallel > len(texts) {
		parallel = len(texts)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, parallel)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for i := range texts {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			score, err := s.Score(ctx, texts[i])
			if err != nil {
				select {
				case errCh <- fmt.Errorf("ollama batch score failed at index %d: %w", i, err):
					cancel()
				default:
				}
				return
			}
			out[i] = score
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
