// Package mock provides a simple scorer implementation for tests.
package mock

import "context"

type scorer struct{}

// NewScorer returns a no-op scorer used in tests.
func NewScorer() *scorer { return &scorer{} }

// Score returns a fixed score of 0.5 for non-empty text.
func (s *scorer) Score(ctx context.Context, text string) (float64, error) {
	_ = ctx
	if text == "" {
		return 0, nil
	}
	return 0.5, nil
}

// BatchScore scores each input text individually.
func (s *scorer) BatchScore(ctx context.Context, texts []string) ([]float64, error) {
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
