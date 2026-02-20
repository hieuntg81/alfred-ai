package llm

import (
	"context"
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
)

// Compile-time interface assertions.
var (
	_ domain.LLMProvider          = (*FailoverProvider)(nil)
	_ domain.StreamingLLMProvider = (*FailoverProvider)(nil)
)

// FailoverProvider wraps a primary LLM provider with fallback providers.
// If the primary fails, it tries each fallback in order.
type FailoverProvider struct {
	primary   domain.LLMProvider
	fallbacks []domain.LLMProvider
	logger    *slog.Logger
}

// NewFailoverProvider creates a failover-capable provider.
func NewFailoverProvider(primary domain.LLMProvider, fallbacks []domain.LLMProvider, logger *slog.Logger) *FailoverProvider {
	return &FailoverProvider{
		primary:   primary,
		fallbacks: fallbacks,
		logger:    logger,
	}
}

// Chat tries the primary provider first, then each fallback on failure.
func (f *FailoverProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	resp, err := f.primary.Chat(ctx, req)
	if err == nil {
		return resp, nil
	}
	f.logger.Warn("primary LLM failed, trying fallbacks",
		"primary", f.primary.Name(), "error", err)

	// Collect all errors for better diagnostics
	allErrors := []string{fmt.Sprintf("%s: %v", f.primary.Name(), err)}

	for _, fb := range f.fallbacks {
		resp, err = fb.Chat(ctx, req)
		if err == nil {
			f.logger.Info("failover succeeded", "provider", fb.Name())
			return resp, nil
		}
		f.logger.Warn("fallback LLM failed", "provider", fb.Name(), "error", err)
		allErrors = append(allErrors, fmt.Sprintf("%s: %v", fb.Name(), err))
	}

	// Return aggregated error with all provider failures
	return nil, fmt.Errorf("all providers failed: [%s]", joinErrors(allErrors))
}

// joinErrors joins error messages with "; " separator
func joinErrors(errors []string) string {
	if len(errors) == 0 {
		return ""
	}
	result := errors[0]
	for i := 1; i < len(errors); i++ {
		result += "; " + errors[i]
	}
	return result
}

// ChatStream tries streaming from the primary, then each fallback.
// It checks whether each provider implements StreamingLLMProvider.
func (f *FailoverProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	var allErrors []string

	if sp, ok := f.primary.(domain.StreamingLLMProvider); ok {
		ch, err := sp.ChatStream(ctx, req)
		if err == nil {
			return ch, nil
		}
		f.logger.Warn("primary streaming LLM failed, trying fallbacks",
			"primary", f.primary.Name(), "error", err)
		allErrors = append(allErrors, fmt.Sprintf("%s: %v", f.primary.Name(), err))
	}

	for _, fb := range f.fallbacks {
		if sp, ok := fb.(domain.StreamingLLMProvider); ok {
			ch, err := sp.ChatStream(ctx, req)
			if err == nil {
				f.logger.Info("streaming failover succeeded", "provider", fb.Name())
				return ch, nil
			}
			f.logger.Warn("fallback streaming LLM failed", "provider", fb.Name(), "error", err)
			allErrors = append(allErrors, fmt.Sprintf("%s: %v", fb.Name(), err))
		}
	}

	if len(allErrors) > 0 {
		return nil, fmt.Errorf("all streaming providers failed: [%s]", joinErrors(allErrors))
	}
	return nil, fmt.Errorf("no streaming-capable providers available")
}

// Name returns a composite name.
func (f *FailoverProvider) Name() string {
	return f.primary.Name() + "+failover"
}
