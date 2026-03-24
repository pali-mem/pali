package domain

import "context"

// Embedder converts text into a vector embedding.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BatchEmbedder is an optional extension for providers that can embed
// multiple texts in one call.
// BatchEmbedder embeds multiple texts in one call.
type BatchEmbedder interface {
	BatchEmbed(ctx context.Context, texts []string) ([][]float32, error)
}
