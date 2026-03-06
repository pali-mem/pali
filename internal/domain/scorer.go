package domain

import "context"

type ImportanceScorer interface {
	Score(ctx context.Context, text string) (float64, error)
}
