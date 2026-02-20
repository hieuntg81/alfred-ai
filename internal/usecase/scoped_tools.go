package usecase

import "alfred-ai/internal/domain"

// NewScopedToolExecutor wraps inner with a filter that only exposes the named
// tools in allowedTools. If allowedTools is nil or empty, inner is returned
// directly (zero overhead pass-through).
func NewScopedToolExecutor(inner domain.ToolExecutor, allowedTools []string) domain.ToolExecutor {
	if len(allowedTools) == 0 {
		return inner
	}
	allowed := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		allowed[name] = true
	}
	return &scopedToolExecutor{inner: inner, allowed: allowed}
}

type scopedToolExecutor struct {
	inner   domain.ToolExecutor
	allowed map[string]bool
}

func (s *scopedToolExecutor) Get(name string) (domain.Tool, error) {
	if !s.allowed[name] {
		return nil, domain.ErrToolNotFound
	}
	return s.inner.Get(name)
}

func (s *scopedToolExecutor) Schemas() []domain.ToolSchema {
	all := s.inner.Schemas()
	filtered := make([]domain.ToolSchema, 0, len(s.allowed))
	for _, schema := range all {
		if s.allowed[schema.Name] {
			filtered = append(filtered, schema)
		}
	}
	return filtered
}
