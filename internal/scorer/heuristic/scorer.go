package heuristic

import (
	"context"
	"math"
	"regexp"
	"strings"
)

type Scorer struct{}

func NewScorer() *Scorer { return &Scorer{} }

var scorerTokens = regexp.MustCompile(`[a-zA-Z0-9_]+`)

func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	_ = ctx
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return 0, nil
	}

	tokens := scorerTokens.FindAllString(text, -1)
	if len(tokens) == 0 {
		return 0, nil
	}

	tf := map[string]int{}
	for _, token := range tokens {
		tf[token]++
	}

	var tfidfLike float64
	for _, count := range tf {
		tfNorm := float64(count) / float64(len(tokens))
		idfLike := 1 + math.Log(1+1/tfNorm)
		tfidfLike += tfNorm * idfLike
	}
	tfidfLike /= float64(len(tf))

	boost := 0.0
	keySignals := []string{
		"prefer", "always", "important", "never", "remember", "must",
	}
	for _, signal := range keySignals {
		if strings.Contains(text, signal) {
			boost += 0.08
		}
	}

	score := tfidfLike + boost
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score, nil
}

func (s *Scorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
	out := make([]float64, 0, len(texts))
	for _, text := range texts {
		score, err := s.Score(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, score)
	}
	return out, nil
}
