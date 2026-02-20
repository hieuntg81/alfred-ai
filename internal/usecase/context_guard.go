package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
)

// ContextGuard prevents context window overflow by checking token usage
// and triggering compression proactively.
type ContextGuard struct {
	maxTokens     int
	reserveTokens int
	safetyMargin  float64 // e.g. 0.15 = 15%
	tokenCounter  domain.TokenCounter
	compressor    *Compressor
	logger        *slog.Logger
}

// ContextGuardConfig holds settings for the context guard.
type ContextGuardConfig struct {
	MaxTokens     int
	ReserveTokens int
	SafetyMargin  float64
}

// NewContextGuard creates a context guard with the given dependencies.
func NewContextGuard(cfg ContextGuardConfig, counter domain.TokenCounter, compressor *Compressor, logger *slog.Logger) *ContextGuard {
	if cfg.SafetyMargin <= 0 {
		cfg.SafetyMargin = 0.15
	}
	if cfg.SafetyMargin > 0.5 {
		cfg.SafetyMargin = 0.5 // clamp: >50% safety margin is unreasonable
	}
	if cfg.ReserveTokens <= 0 {
		cfg.ReserveTokens = 1000
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 128000 // sensible default for GPT-4 class models
	}
	return &ContextGuard{
		maxTokens:     cfg.MaxTokens,
		reserveTokens: cfg.ReserveTokens,
		safetyMargin:  cfg.SafetyMargin,
		tokenCounter:  counter,
		compressor:    compressor,
		logger:        logger,
	}
}

// Check evaluates the session's token usage against limits.
// If over the safe threshold, it attempts compression.
// Returns domain.ErrContextOverflow if still over limit after compression.
func (g *ContextGuard) Check(ctx context.Context, session *Session) error {
	tokens := g.tokenCounter.CountMessages(session.Messages())
	limit := int(float64(g.maxTokens)*(1-g.safetyMargin)) - g.reserveTokens

	if tokens <= limit {
		return nil
	}

	g.logger.Warn("context guard: token limit approaching, attempting compression",
		"tokens", tokens,
		"limit", limit,
		"max_tokens", g.maxTokens,
	)

	// Try compression if available.
	if g.compressor != nil {
		if compErr := g.compressor.ForceCompress(ctx, session); compErr != nil {
			g.logger.Error("context guard: compression failed", "error", compErr)
			// Compression failed and we're still over limit.
			return fmt.Errorf("context guard: compression failed (%w): %w", compErr, domain.ErrContextOverflow)
		}

		// Re-check after compression.
		tokens = g.tokenCounter.CountMessages(session.Messages())
		if tokens <= limit {
			g.logger.Info("context guard: compression resolved overflow",
				"tokens_after", tokens,
				"limit", limit,
			)
			return nil
		}
	}

	g.logger.Error("context guard: context overflow",
		"tokens", tokens,
		"limit", limit,
	)
	return domain.ErrContextOverflow
}
