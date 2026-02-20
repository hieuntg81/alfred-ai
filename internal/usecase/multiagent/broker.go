package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"alfred-ai/internal/domain"
)

// DelegateRequest represents a cross-agent delegation.
type DelegateRequest struct {
	FromAgent string `json:"from_agent"`
	ToAgent   string `json:"to_agent"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// DelegateResponse is the result of a delegation.
type DelegateResponse struct {
	FromAgent string `json:"from_agent"`
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
}

// Broker orchestrates cross-agent communication.
type Broker struct {
	registry *Registry
	bus      domain.EventBus
	logger   *slog.Logger
}

// NewBroker creates a Broker for cross-agent delegation.
func NewBroker(registry *Registry, bus domain.EventBus, logger *slog.Logger) *Broker {
	return &Broker{
		registry: registry,
		bus:      bus,
		logger:   logger,
	}
}

// Delegate sends a message from one agent to another and returns the response.
// Sessions are isolated using a composite key: delegate|<from>|<to>|<sessionID>.
func (b *Broker) Delegate(ctx context.Context, req DelegateRequest) (*DelegateResponse, error) {
	inst, err := b.registry.Get(req.ToAgent)
	if err != nil {
		return nil, fmt.Errorf("broker: target agent %q: %w", req.ToAgent, err)
	}

	// Isolated session key prevents cross-contamination.
	// Uses "|" as delimiter to avoid collision with agent IDs containing ":".
	sessionKey := fmt.Sprintf("delegate|%s|%s|%s", req.FromAgent, req.ToAgent, req.SessionID)
	session := inst.Sessions.GetOrCreate(sessionKey)

	// Publish delegation event (use session ULID for consistency with agent events).
	b.publishEvent(ctx, session.ID, req)

	b.logger.Info("delegating",
		"from", req.FromAgent,
		"to", req.ToAgent,
		"session", sessionKey,
	)

	response, err := inst.Agent.HandleMessage(ctx, session, req.Message)
	if err != nil {
		return nil, fmt.Errorf("broker: agent %q: %w", req.ToAgent, err)
	}

	return &DelegateResponse{
		FromAgent: req.ToAgent,
		Content:   response,
	}, nil
}

func (b *Broker) publishEvent(ctx context.Context, sessionID string, req DelegateRequest) {
	if b.bus == nil {
		return
	}
	payload, err := json.Marshal(req)
	if err != nil {
		b.logger.Warn("broker: failed to marshal delegation event", "error", err)
		return
	}
	b.bus.Publish(ctx, domain.Event{
		Type:      domain.EventAgentDelegated,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   payload,
	})
}
