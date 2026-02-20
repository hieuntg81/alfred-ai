//go:build !vector_memory

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildVectorMemory(_ config.MemoryConfig, _ *slog.Logger) (domain.MemoryProvider, func() error, error) {
	return nil, nil, fmt.Errorf("vector memory requires build with -tags vector_memory")
}
