package multiagent

import (
	"log/slog"
	"sort"
	"sync"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

// AgentInstance bundles a running agent with its isolated dependencies.
type AgentInstance struct {
	Identity  domain.AgentIdentity
	Agent     *usecase.Agent
	Sessions  *usecase.SessionManager
	Workspace string // path to agent's data directory
}

// Registry holds all registered agent instances and provides lookup.
type Registry struct {
	mu        sync.RWMutex
	agents    map[string]*AgentInstance
	defaultID string
	logger    *slog.Logger
}

// NewRegistry creates a Registry with the given default agent ID.
func NewRegistry(defaultID string, logger *slog.Logger) *Registry {
	return &Registry{
		agents:    make(map[string]*AgentInstance),
		defaultID: defaultID,
		logger:    logger,
	}
}

// Register adds an agent instance. Returns ErrDuplicate if already registered.
func (r *Registry) Register(instance *AgentInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := instance.Identity.ID
	if _, exists := r.agents[id]; exists {
		return domain.ErrDuplicate
	}
	r.agents[id] = instance
	r.logger.Info("agent registered", "agent_id", id, "name", instance.Identity.Name)
	return nil
}

// Get returns the agent instance for the given ID, or ErrNotFound.
func (r *Registry) Get(agentID string) (*AgentInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	inst, ok := r.agents[agentID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return inst, nil
}

// Default returns the default agent instance.
func (r *Registry) Default() (*AgentInstance, error) {
	return r.Get(r.defaultID)
}

// List returns a status snapshot for every registered agent, sorted by ID.
func (r *Registry) List() []domain.AgentStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	statuses := make([]domain.AgentStatus, 0, len(r.agents))
	for _, inst := range r.agents {
		statuses = append(statuses, domain.AgentStatus{
			ID:             inst.Identity.ID,
			Name:           inst.Identity.Name,
			Provider:       inst.Identity.Provider,
			Model:          inst.Identity.Model,
			ActiveSessions: len(inst.Sessions.ListSessions()),
		})
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ID < statuses[j].ID
	})
	return statuses
}

// Remove unregisters an agent. Returns ErrNotFound if not present.
func (r *Registry) Remove(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[agentID]; !ok {
		return domain.ErrNotFound
	}
	delete(r.agents, agentID)
	r.logger.Info("agent removed", "agent_id", agentID)
	return nil
}

// Lookup returns an AgentLookup closure suitable for use with usecase.Router.
func (r *Registry) Lookup() func(agentID string) (*usecase.Agent, *usecase.SessionManager, error) {
	return func(agentID string) (*usecase.Agent, *usecase.SessionManager, error) {
		inst, err := r.Get(agentID)
		if err != nil {
			return nil, nil, err
		}
		return inst.Agent, inst.Sessions, nil
	}
}
