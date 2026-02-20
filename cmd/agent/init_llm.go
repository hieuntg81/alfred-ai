package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// LLMComponents holds all LLM-related components
type LLMComponents struct {
	Registry   *llm.Registry
	DefaultLLM domain.LLMProvider
}

// initLLM initializes LLM providers, registry, and failover
// Returns the components and any error
func initLLM(cfg *config.Config, log *slog.Logger) (*LLMComponents, error) {
	// 1. Create LLM registry
	registry := llm.NewRegistry()

	// 2. Register all configured providers
	cbCfg := cfg.LLM.CircuitBreaker
	for _, pc := range cfg.LLM.Providers {
		provider, err := createLLMProvider(pc, log)
		if err != nil {
			return nil, fmt.Errorf("llm provider %s: %w", pc.Name, err)
		}

		// Wrap with circuit breaker if enabled (per-provider).
		if cbCfg.Enabled {
			provider = llm.NewCircuitBreakerProvider(provider, llm.CircuitBreakerConfig{
				MaxFailures: cbCfg.MaxFailures,
				Timeout:     cbCfg.Timeout,
				Interval:    cbCfg.Interval,
			}, log)
		}

		if err := registry.Register(provider); err != nil {
			return nil, fmt.Errorf("llm provider %s: %w", pc.Name, err)
		}
	}

	if cbCfg.Enabled {
		log.Info("llm circuit breaker enabled",
			"max_failures", cbCfg.MaxFailures,
			"timeout", cbCfg.Timeout,
			"interval", cbCfg.Interval,
		)
	}

	// 3. Get default provider
	defaultLLM, err := registry.Get(cfg.LLM.DefaultProvider)
	if err != nil {
		return nil, fmt.Errorf("default llm provider: %w", err)
	}

	// 4. Wrap with failover if enabled
	if cfg.LLM.Failover.Enabled && len(cfg.LLM.Failover.Fallbacks) > 0 {
		var fallbacks []domain.LLMProvider
		for _, name := range cfg.LLM.Failover.Fallbacks {
			fb, err := registry.Get(name)
			if err != nil {
				return nil, fmt.Errorf("failover provider %s: %w", name, err)
			}
			fallbacks = append(fallbacks, fb)
		}
		defaultLLM = llm.NewFailoverProvider(defaultLLM, fallbacks, log)
		log.Info("model failover enabled", "fallbacks", cfg.LLM.Failover.Fallbacks)
	}

	return &LLMComponents{
		Registry:   registry,
		DefaultLLM: defaultLLM,
	}, nil
}
