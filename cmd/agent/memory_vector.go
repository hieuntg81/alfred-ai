//go:build vector_memory

package main

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"alfred-ai/internal/adapter/embedding"
	"alfred-ai/internal/adapter/memory/vector"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildVectorMemory(cfg config.MemoryConfig, log *slog.Logger) (domain.MemoryProvider, func() error, error) {
	var embedder domain.EmbeddingProvider

	if cfg.Embedding.Provider != "" {
		var err error
		embedder, err = createEmbedder(cfg.Embedding)
		if err != nil {
			return nil, nil, fmt.Errorf("embedding provider: %w", err)
		}
		log.Info("embedding provider initialized",
			"provider", embedder.Name(),
			"dimensions", embedder.Dimensions(),
		)

		// Wrap with embedding cache if configured.
		if cfg.Search.EmbeddingCacheSize > 0 {
			embedder = embedding.NewCachedEmbedder(embedder, cfg.Search.EmbeddingCacheSize)
			log.Info("embedding cache enabled", "size", cfg.Search.EmbeddingCacheSize)
		}
	}

	// Map search config to vector store options.
	var searchOpts []vector.SearchOpts
	if cfg.Search.DecayHalfLife > 0 || cfg.Search.MMRDiversity > 0 || cfg.Search.MaxVectorCandidates > 0 {
		searchOpts = append(searchOpts, vector.SearchOpts{
			DecayHalfLife:       cfg.Search.DecayHalfLife,
			MMRDiversity:        cfg.Search.MMRDiversity,
			MaxVectorCandidates: cfg.Search.MaxVectorCandidates,
		})
	}

	dbPath := filepath.Join(cfg.DataDir, "vector.db")
	store, err := vector.New(dbPath, embedder, log, searchOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("vector store: %w", err)
	}

	return store, store.Close, nil
}

func createEmbedder(cfg config.EmbeddingConfig) (domain.EmbeddingProvider, error) {
	switch cfg.Provider {
	case "openai":
		var opts []embedding.OpenAIOption
		if cfg.Model != "" {
			opts = append(opts, embedding.WithOpenAIModel(cfg.Model))
		}
		return embedding.NewOpenAIProvider(cfg.APIKey, opts...), nil
	case "gemini":
		var opts []embedding.GeminiOption
		if cfg.Model != "" {
			opts = append(opts, embedding.WithGeminiModel(cfg.Model))
		}
		return embedding.NewGeminiProvider(cfg.APIKey, opts...), nil
	case "ollama":
		var opts []embedding.OllamaOption
		if cfg.Model != "" {
			opts = append(opts, embedding.WithOllamaModel(cfg.Model))
		}
		return embedding.NewOllamaProvider(opts...), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider: %q", cfg.Provider)
	}
}
