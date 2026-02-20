package multiagent

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"alfred-ai/internal/domain"
)

// discardLogger returns a no-op logger for routers created without one.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// DefaultRouter always returns the default agent ID.
type DefaultRouter struct {
	defaultID string
	logger    *slog.Logger
}

// NewDefaultRouter creates a router that always routes to defaultID.
func NewDefaultRouter(defaultID string) *DefaultRouter {
	return &DefaultRouter{defaultID: defaultID, logger: discardLogger()}
}

// NewDefaultRouterWithLogger creates a DefaultRouter with debug logging.
func NewDefaultRouterWithLogger(defaultID string, logger *slog.Logger) *DefaultRouter {
	return &DefaultRouter{defaultID: defaultID, logger: logger}
}

func (r *DefaultRouter) Route(_ context.Context, msg domain.InboundMessage) (string, error) {
	r.logger.Debug("routing to default agent", "agent_id", r.defaultID, "channel", msg.ChannelName)
	return r.defaultID, nil
}

// PrefixRouter parses an @agent-name prefix from the message content.
// Falls back to defaultID when no prefix or unknown agent.
type PrefixRouter struct {
	defaultID string
	known     map[string]string // name -> agentID
	logger    *slog.Logger
}

// NewPrefixRouter creates a router that parses @agent-name prefixes.
// agentNames maps lowercase agent names to their IDs.
func NewPrefixRouter(defaultID string, agentNames map[string]string) *PrefixRouter {
	return &PrefixRouter{defaultID: defaultID, known: agentNames, logger: discardLogger()}
}

// NewPrefixRouterWithLogger creates a PrefixRouter with debug logging.
func NewPrefixRouterWithLogger(defaultID string, agentNames map[string]string, logger *slog.Logger) *PrefixRouter {
	return &PrefixRouter{defaultID: defaultID, known: agentNames, logger: logger}
}

func (r *PrefixRouter) Route(_ context.Context, msg domain.InboundMessage) (string, error) {
	content := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(content, "@") {
		r.logger.Debug("no @prefix, routing to default", "agent_id", r.defaultID)
		return r.defaultID, nil
	}

	// Extract the name after @, up to the first space.
	rest := content[1:]
	name := rest
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		name = rest[:idx]
	}
	name = strings.ToLower(name)

	if agentID, ok := r.known[name]; ok {
		r.logger.Debug("prefix matched agent", "prefix", name, "agent_id", agentID)
		return agentID, nil
	}
	r.logger.Debug("unknown prefix, routing to default", "prefix", name, "agent_id", r.defaultID)
	return r.defaultID, nil
}

// RoutingRule maps a (channel, group) pair to an agent ID.
type RoutingRule struct {
	Channel string // channel name or "*" for any
	GroupID string // group ID or "*" for any
	AgentID string
}

// ConfigRouter matches inbound messages against a list of routing rules.
// The first matching rule wins. Falls back to defaultID when nothing matches.
type ConfigRouter struct {
	defaultID string
	rules     []RoutingRule
	logger    *slog.Logger
}

// NewConfigRouter creates a router that uses configured rules.
func NewConfigRouter(defaultID string, rules []RoutingRule) *ConfigRouter {
	return &ConfigRouter{defaultID: defaultID, rules: rules, logger: discardLogger()}
}

// NewConfigRouterWithLogger creates a ConfigRouter with debug logging.
func NewConfigRouterWithLogger(defaultID string, rules []RoutingRule, logger *slog.Logger) *ConfigRouter {
	return &ConfigRouter{defaultID: defaultID, rules: rules, logger: logger}
}

func (r *ConfigRouter) Route(_ context.Context, msg domain.InboundMessage) (string, error) {
	for _, rule := range r.rules {
		channelMatch := rule.Channel == "*" || rule.Channel == msg.ChannelName
		groupMatch := rule.GroupID == "*" || rule.GroupID == msg.GroupID
		if channelMatch && groupMatch {
			r.logger.Debug("routing rule matched", "channel", msg.ChannelName, "group", msg.GroupID, "agent_id", rule.AgentID)
			return rule.AgentID, nil
		}
	}
	r.logger.Debug("no routing rule matched, using default", "channel", msg.ChannelName, "group", msg.GroupID, "agent_id", r.defaultID)
	return r.defaultID, nil
}
