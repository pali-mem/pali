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
