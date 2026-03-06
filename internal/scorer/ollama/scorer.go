package ollama

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Scorer struct {
	client *Client
}

func NewScorer(c *Client) *Scorer { return &Scorer{client: c} }

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)`)
var thinkBlockPattern = regexp.MustCompile(`(?is)<think>.*?</think>`)
var thinkTagPattern = regexp.MustCompile(`(?i)</?think>`)

func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("ollama scorer is not configured")
	}
	content := strings.TrimSpace(text)
	if content == "" {
		return 0, nil
	}

	prompt := buildScorePrompt(content)
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

func buildScorePrompt(content string) string {
	return "You are scoring memory importance for long-term retrieval.\n" +
		"Return only one decimal number between 0 and 1.\n" +
		"0 means disposable or low-value context.\n" +
		"1 means durable user preference/profile or critical instruction.\n\n" +
		"Memory:\n" + content
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
