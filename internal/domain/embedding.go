package domain

import "context"

// EmbeddingProvider is the interface for text embedding backends.
type EmbeddingProvider interface {
	// Embed generates embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
	// Name returns the provider's identifier (e.g., "openai", "gemini").
	Name() string
}
