package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
)

type MultiHopQueryDecomposer interface {
	Decompose(ctx context.Context, query string, maxQueries int) ([]string, error)
}

type llmPromptGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
	Model() string
}

type llmMultiHopQueryDecomposer struct {
	generator llmPromptGenerator
	logger    *log.Logger
	verbose   bool
}

func NewLLMMultiHopQueryDecomposer(
	generator llmPromptGenerator,
	logger *log.Logger,
	verbose bool,
) MultiHopQueryDecomposer {
	return &llmMultiHopQueryDecomposer{
		generator: generator,
		logger:    logger,
		verbose:   verbose,
	}
}

func (d *llmMultiHopQueryDecomposer) Decompose(ctx context.Context, query string, maxQueries int) ([]string, error) {
	if d == nil || d.generator == nil {
		return nil, fmt.Errorf("multi-hop decomposer is not configured")
	}
	if maxQueries <= 0 {
		return nil, fmt.Errorf("max queries must be > 0")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []string{}, nil
	}

	start := time.Now()
	raw, err := d.generator.Generate(ctx, coreprompts.MultiHopDecomposition(query, maxQueries))
	if err != nil {
		d.debugf("[pali-search] multihop_decompose model=%s status=error ms=%d err=%v", d.generator.Model(), time.Since(start).Milliseconds(), err)
		return nil, err
	}
	queries, err := parseMultiHopDecomposition(raw, maxQueries)
	if err != nil {
		d.debugf("[pali-search] multihop_decompose model=%s status=parse_error ms=%d err=%v", d.generator.Model(), time.Since(start).Milliseconds(), err)
		return nil, err
	}
	d.debugf("[pali-search] multihop_decompose model=%s status=ok ms=%d produced=%d", d.generator.Model(), time.Since(start).Milliseconds(), len(queries))
	return queries, nil
}

func (d *llmMultiHopQueryDecomposer) debugf(format string, args ...any) {
	if d == nil || d.logger == nil || !d.verbose {
		return
	}
	d.logger.Printf(format, args...)
}

func parseMultiHopDecomposition(raw string, maxQueries int) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty decomposition response")
	}

	type payload struct {
		SubQueries []string `json:"sub_queries"`
	}
	tryDecode := func(input string) ([]string, bool) {
		var p payload
		if err := json.Unmarshal([]byte(input), &p); err == nil && len(p.SubQueries) > 0 {
			return p.SubQueries, true
		}
		var arr []string
		if err := json.Unmarshal([]byte(input), &arr); err == nil && len(arr) > 0 {
			return arr, true
		}
		return nil, false
	}

	if decoded, ok := tryDecode(trimmed); ok {
		return sanitizeDecomposedQueries(decoded, maxQueries), nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		if decoded, ok := tryDecode(trimmed[start : end+1]); ok {
			return sanitizeDecomposedQueries(decoded, maxQueries), nil
		}
	}
	arrStart := strings.Index(trimmed, "[")
	arrEnd := strings.LastIndex(trimmed, "]")
	if arrStart >= 0 && arrEnd > arrStart {
		if decoded, ok := tryDecode(trimmed[arrStart : arrEnd+1]); ok {
			return sanitizeDecomposedQueries(decoded, maxQueries), nil
		}
	}
	return nil, fmt.Errorf("decomposition response is not valid JSON")
}

func sanitizeDecomposedQueries(queries []string, maxQueries int) []string {
	out := make([]string, 0, min(maxQueries, len(queries)))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		query = condenseSearchQuery(strings.TrimSpace(query))
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, query)
		if len(out) >= maxQueries {
			break
		}
	}
	return out
}
