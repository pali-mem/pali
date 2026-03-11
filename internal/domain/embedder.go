package domain

import "context"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BatchEmbedder is an optional extension for providers that can embed
// multiple texts in one call.
type BatchEmbedder interface {
	BatchEmbed(ctx context.Context, texts []string) ([][]float32, error)
}
