package domain_test

import (
	"context"

	"alfred-ai/internal/domain"
)

// Compile-time interface check.
var _ domain.EmbeddingProvider = (*stubEmbedder)(nil)

type stubEmbedder struct{}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, 3)
	}
	return out, nil
}

func (s *stubEmbedder) Dimensions() int { return 3 }
func (s *stubEmbedder) Name() string    { return "stub" }
