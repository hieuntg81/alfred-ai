package tool

import (
	"fmt"
	"log/slog"
	"sync"

	"alfred-ai/internal/domain"
)

// Registry holds named tools.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]domain.Tool
	logger *slog.Logger
}

// NewRegistry creates an empty tool registry.
// If logger is non-nil, tools are wrapped with schema validation on Register;
// compilation errors are logged and the tool is registered unwrapped.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		tools:  make(map[string]domain.Tool),
		logger: logger,
	}
}

// Register adds a tool. Returns error if name already registered.
// If the registry was created with a logger, the tool is wrapped with
// schema validation. If schema compilation fails, the tool is registered
// without validation and a warning is logged.
func (r *Registry) Register(t domain.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}

	if r.logger != nil {
		wrapped, err := WithSchemaValidation(t)
		if err != nil {
			r.logger.Warn("schema validation disabled for tool",
				"tool", name, "error", err)
		} else {
			t = wrapped
		}
	}

	r.tools[name] = t
	return nil
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (domain.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	if !ok {
		return nil, domain.NewDomainError("Registry.Get", domain.ErrToolNotFound, name)
	}
	return t, nil
}

// List returns all registered tools.
func (r *Registry) List() []domain.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]domain.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Schemas returns all tool schemas for LLM function-calling.
func (r *Registry) Schemas() []domain.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]domain.ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, t.Schema())
	}
	return schemas
}
