package llm

import (
	"fmt"
	"sync"

	"alfred-ai/internal/domain"
)

// Registry holds named LLM providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]domain.LLMProvider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]domain.LLMProvider),
	}
}

// Register adds a provider. Returns error if name already registered.
func (r *Registry) Register(provider domain.LLMProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := provider.Name()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = provider
	return nil
}

// Get retrieves a provider by name.
func (r *Registry) Get(name string) (domain.LLMProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[name]
	if !ok {
		return nil, domain.NewDomainError("Registry.Get", domain.ErrProviderNotFound, name)
	}
	return p, nil
}

// List returns all registered provider names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
