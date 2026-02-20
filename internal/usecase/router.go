package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// AgentLookup resolves an agent ID to its Agent and SessionManager.
// Used in multi-agent mode to avoid import cycles with the multiagent package.
type AgentLookup func(agentID string) (*Agent, *SessionManager, error)

// SecretScanner is the interface for message secret scanning.
type SecretScanner interface {
	Apply(text string) (cleaned string, blocked bool, matches []SecretMatch)
}

// SecretMatch holds details about a detected secret.
type SecretMatch struct {
	PatternName string
	Action      string
	Start       int
	End         int
}

// Router dispatches inbound messages from any channel through the agent,
// normalizing session keys, invoking hooks, publishing events, and
// auto-curating when configured.
type Router struct {
	// Single-agent mode fields (both nil in multi-agent mode).
	agent    *Agent
	sessions *SessionManager

	// Multi-agent mode fields (both nil in single-agent mode).
	lookup      AgentLookup        // resolves agentID → (Agent, SessionManager)
	agentRouter domain.AgentRouter // decides which agent handles a message

	bus        domain.EventBus
	hooks      []domain.PluginHook
	curator    *Curator
	scanner    SecretScanner
	offline    *OfflineManager
	authorizer domain.Authorizer   // nil = skip RBAC checks
	logger     *slog.Logger
	wg         sync.WaitGroup    // tracks background goroutines (auto-curate)
	onboarding *OnboardingHelper // tracks first contact and provides welcome messages
}

// NewRouter creates a single-agent Router. Hooks and curator are optional
// and can be set after construction via SetHooks / SetCurator.
func NewRouter(agent *Agent, sessions *SessionManager, bus domain.EventBus, logger *slog.Logger) *Router {
	return &Router{
		agent:      agent,
		sessions:   sessions,
		bus:        bus,
		logger:     logger,
		onboarding: NewOnboardingHelper(),
	}
}

// NewMultiRouter creates a multi-agent Router that dispatches messages
// to the agent selected by agentRouter, looking up the concrete Agent
// and SessionManager via the lookup function.
func NewMultiRouter(lookup AgentLookup, agentRouter domain.AgentRouter, bus domain.EventBus, logger *slog.Logger) *Router {
	return &Router{
		lookup:      lookup,
		agentRouter: agentRouter,
		bus:         bus,
		logger:      logger,
		onboarding:  NewOnboardingHelper(),
	}
}

// SetHooks replaces the hook list.
func (r *Router) SetHooks(hooks []domain.PluginHook) { r.hooks = hooks }

// SetCurator enables post-response auto-curation.
func (r *Router) SetCurator(curator *Curator) { r.curator = curator }

// SetScanner enables secret scanning on inbound messages.
func (r *Router) SetScanner(scanner SecretScanner) { r.scanner = scanner }

// SetOffline enables offline fallback via the given OfflineManager.
func (r *Router) SetOffline(om *OfflineManager) { r.offline = om }

// SetAuthorizer enables service-layer RBAC. If nil, RBAC checks are skipped.
func (r *Router) SetAuthorizer(auth domain.Authorizer) { r.authorizer = auth }

// Handle processes one inbound message end-to-end and returns the outbound
// response. It is safe to call concurrently.
func (r *Router) Handle(ctx context.Context, msg domain.InboundMessage) (domain.OutboundMessage, error) {
	return r.handleInner(ctx, msg, false)
}

// HandleStream processes an inbound message with token-by-token streaming.
// The agent publishes EventStreamDelta events as LLM tokens arrive.
// The final OutboundMessage contains the complete response.
func (r *Router) HandleStream(ctx context.Context, msg domain.InboundMessage) (domain.OutboundMessage, error) {
	return r.handleInner(ctx, msg, true)
}

// handleInner is the shared implementation for Handle and HandleStream.
func (r *Router) handleInner(ctx context.Context, msg domain.InboundMessage, stream bool) (domain.OutboundMessage, error) {
	// Service-layer RBAC: verify the caller has permission to execute tools.
	if r.authorizer != nil {
		roles := domain.RolesFromContext(ctx)
		if len(roles) > 0 {
			if err := r.authorizer.Authorize(ctx, roles, domain.PermToolExecute); err != nil {
				return domain.OutboundMessage{}, err
			}
		}
	}

	// Resolve agent and session manager (single- or multi-agent).
	agent, sessions, err := r.resolveAgent(ctx, msg)
	if err != nil {
		return domain.OutboundMessage{}, domain.WrapOp("route", err)
	}

	// 1. Normalize session key: channelName:sessionID
	sessionKey := msg.ChannelName + ":" + msg.SessionID

	// 2. Secret scanning (before any processing).
	if r.scanner != nil {
		cleaned, blocked, matches := r.scanner.Apply(msg.Content)
		if blocked {
			return domain.OutboundMessage{
				SessionID: msg.SessionID,
				Content:   "Message blocked: contains sensitive data that cannot be processed.",
				IsError:   true,
			}, nil
		}
		if len(matches) > 0 {
			r.logger.Warn("secrets detected in message", "matches", len(matches), "channel", msg.ChannelName)
			msg.Content = cleaned
		}
	}

	// 3. Get or create session.
	session := sessions.GetOrCreate(sessionKey)

	// 4. Invoke OnMessageReceived hooks (pass by value).
	for _, h := range r.hooks {
		if err := h.OnMessageReceived(ctx, msg); err != nil {
			r.logger.Warn("hook OnMessageReceived error", "error", err)
			// Continue — hook errors are non-fatal.
		}
	}

	// 5. Publish EventMessageReceived.
	r.publishEvent(ctx, domain.EventMessageReceived, session.ID, nil)

	// 6. Call agent (streaming or synchronous).
	var response string
	if stream {
		response, err = agent.HandleMessageStream(ctx, session, msg.Content)
	} else {
		response, err = agent.HandleMessage(ctx, session, msg.Content)
	}
	if err != nil {
		// 6a. Offline fallback: if agent call fails and offline manager is
		// available, try the local LLM instead.
		if r.offline != nil && !r.offline.IsOnline() {
			r.logger.Info("agent call failed while offline, using local LLM fallback",
				"error", err, "session", sessionKey)
			offlineResp, offErr := r.offline.HandleOffline(ctx, sessionKey, msg)
			if offErr != nil {
				return domain.OutboundMessage{}, fmt.Errorf("offline fallback: %w", offErr)
			}
			response = offlineResp
			err = nil
		} else {
			return domain.OutboundMessage{}, domain.WrapOp("agent", err)
		}
	}

	// 7. Build outbound message (SessionID = original, not normalized).
	out := domain.OutboundMessage{
		SessionID: msg.SessionID,
		Content:   response,
	}

	// 7a. Add welcome message or progressive hints (onboarding UX).
	if r.onboarding != nil {
		msgCount := session.MessageCount()

		// First interaction: prepend welcome message
		if r.onboarding.IsFirstContact(sessionKey) && msgCount == 2 {
			r.onboarding.MarkContacted(sessionKey)
			welcome := GetWelcomeMessage(msg.ChannelName)
			out.Content = welcome + "\n\n" + out.Content
		}

		// Progressive hints at milestones
		if hint := GetHintForMilestone(msgCount); hint != "" {
			out.Content = out.Content + "\n\n" + hint
		}
	}

	// 8. Invoke OnResponseReady hooks — adapt string interface.
	for _, h := range r.hooks {
		modified, hookErr := h.OnResponseReady(ctx, out.Content)
		if hookErr != nil {
			r.logger.Warn("hook OnResponseReady error", "error", hookErr)
			continue
		}
		out.Content = modified
	}

	// 9. Publish EventMessageSent.
	r.publishEvent(ctx, domain.EventMessageSent, session.ID, nil)

	// 10. Save session + fire-and-forget auto-curate.
	if saveErr := sessions.Save(sessionKey); saveErr != nil {
		r.logger.Warn("failed to save session", "error", saveErr)
	}

	if r.curator != nil {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					r.logger.Error("curator panicked", "panic", rec)
				}
			}()

			// Inherit parent context but add timeout
			curateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			start := time.Now()
			result, curErr := r.curator.CurateConversation(curateCtx, session.Messages())

			if curErr != nil {
				r.logger.Warn("auto-curate failed",
					"error", curErr,
					"duration", time.Since(start))
				return
			}
			if result != nil && result.Stored > 0 {
				r.logger.Info("auto-curate completed",
					"memories_created", result.Stored,
					"keywords", result.Keywords,
					"duration", time.Since(start))
			}
		}()
	}

	// 11. Return outbound message.
	return out, nil
}

// Wait blocks until all background goroutines (auto-curate) complete.
// Call during shutdown to avoid orphaned goroutines.
func (r *Router) Wait() { r.wg.Wait() }

// resolveAgent returns the Agent and SessionManager for the given message.
// In single-agent mode it returns the Router's own agent/sessions.
// In multi-agent mode it routes via agentRouter then looks up the result.
func (r *Router) resolveAgent(ctx context.Context, msg domain.InboundMessage) (*Agent, *SessionManager, error) {
	if r.lookup == nil {
		// Single-agent mode.
		return r.agent, r.sessions, nil
	}
	agentID, err := r.agentRouter.Route(ctx, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("agent router: %w", err)
	}
	r.publishEvent(ctx, domain.EventAgentRouted, msg.SessionID, map[string]string{"agent_id": agentID})
	return r.lookup(agentID)
}

func (r *Router) publishEvent(ctx context.Context, eventType domain.EventType, sessionID string, payload any) {
	publishEvent(r.bus, ctx, eventType, sessionID, payload)
}

// publishEvent is the shared event publishing helper for the usecase layer.
// If bus is nil, this is a no-op.
func publishEvent(bus domain.EventBus, ctx context.Context, eventType domain.EventType, sessionID string, payload any) {
	if bus == nil {
		return
	}
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err == nil {
			raw = data
		}
	}
	bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   raw,
	})
}
