package mock

import "context"

type Scorer struct{}

func NewScorer() *Scorer { return &Scorer{} }

func (s *Scorer) Score(ctx context.Context, text string) (float64, error) {
	_ = ctx
	if text == "" {
		return 0, nil
	}
	return 0.5, nil
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
