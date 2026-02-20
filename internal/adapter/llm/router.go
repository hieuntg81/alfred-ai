package llm

import (
	"fmt"

	"alfred-ai/internal/domain"
)

// PreferenceRouter maps model preference labels to concrete LLM providers.
// It implements domain.ModelRouter.
type PreferenceRouter struct {
	mapping  map[string]string // preference → provider name
	registry *Registry
	fallback domain.LLMProvider
}

// NewPreferenceRouter creates a router from a mapping and a provider registry.
// The mapping maps preference labels (e.g. "fast", "powerful") to provider names.
// The fallback is used when a preference maps to "default" or is empty.
func NewPreferenceRouter(mapping map[string]string, registry *Registry, fallback domain.LLMProvider) *PreferenceRouter {
	return &PreferenceRouter{
		mapping:  mapping,
		registry: registry,
		fallback: fallback,
	}
}

// Route resolves a preference label to an LLM provider.
func (r *PreferenceRouter) Route(preference string) (domain.LLMProvider, error) {
	if preference == "" || preference == "default" {
		if r.fallback != nil {
			return r.fallback, nil
		}
		return nil, fmt.Errorf("no default provider configured")
	}

	providerName, ok := r.mapping[preference]
	if !ok {
		// Unknown preference: fall back to default.
		if r.fallback != nil {
			return r.fallback, nil
		}
		return nil, fmt.Errorf("unknown model preference %q and no fallback", preference)
	}

	if providerName == "" || providerName == "default" {
		if r.fallback != nil {
			return r.fallback, nil
		}
		return nil, fmt.Errorf("preference %q maps to default but no fallback", preference)
	}

	provider, err := r.registry.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("preference %q: provider %q: %w", preference, providerName, err)
	}
	return provider, nil
}
