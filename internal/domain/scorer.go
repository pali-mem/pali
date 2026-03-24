package domain

import "context"

// ImportanceScorer scores a single memory for long-term importance.
type ImportanceScorer interface {
	Score(ctx context.Context, text string) (float64, error)
}

// BatchImportanceScorer is an optional extension for scorers that can score
// multiple texts in one call.
type BatchImportanceScorer interface {
	BatchScore(ctx context.Context, texts []string) ([]float64, error)
}
