package domain

import "context"

type ImportanceScorer interface {
	Score(ctx context.Context, text string) (float64, error)
}

// BatchImportanceScorer is an optional extension for scorers that can score
// multiple texts in one call.
type BatchImportanceScorer interface {
	BatchScore(ctx context.Context, texts []string) ([]float64, error)
}
