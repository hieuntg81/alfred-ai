package multiagent

import (
	"context"
	"testing"

	"alfred-ai/internal/domain"
)

func TestDefaultRouterAlways(t *testing.T) {
	r := NewDefaultRouter("main")
	id, err := r.Route(context.Background(), domain.InboundMessage{Content: "hello"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "main" {
		t.Errorf("got %q, want %q", id, "main")
	}
}

func TestPrefixRouterWithAt(t *testing.T) {
	r := NewPrefixRouter("main", map[string]string{
		"support": "support-agent",
		"sales":   "sales-agent",
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{Content: "@support help me"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "support-agent" {
		t.Errorf("got %q, want %q", id, "support-agent")
	}
}

func TestPrefixRouterNoPrefix(t *testing.T) {
	r := NewPrefixRouter("main", map[string]string{
		"support": "support-agent",
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{Content: "just a question"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "main" {
		t.Errorf("got %q, want %q", id, "main")
	}
}

func TestPrefixRouterUnknown(t *testing.T) {
	r := NewPrefixRouter("main", map[string]string{
		"support": "support-agent",
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{Content: "@unknown hello"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "main" {
		t.Errorf("unknown prefix should fall back to default, got %q", id)
	}
}

func TestPrefixRouterEmpty(t *testing.T) {
	r := NewPrefixRouter("main", map[string]string{})
	id, err := r.Route(context.Background(), domain.InboundMessage{Content: ""})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "main" {
		t.Errorf("got %q, want %q", id, "main")
	}
}

func TestConfigRouterExact(t *testing.T) {
	r := NewConfigRouter("main", []RoutingRule{
		{Channel: "slack", GroupID: "C123", AgentID: "support"},
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{
		ChannelName: "slack",
		GroupID:     "C123",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "support" {
		t.Errorf("got %q, want %q", id, "support")
	}
}

func TestConfigRouterWildcardChannel(t *testing.T) {
	r := NewConfigRouter("main", []RoutingRule{
		{Channel: "*", GroupID: "G456", AgentID: "global"},
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{
		ChannelName: "telegram",
		GroupID:     "G456",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "global" {
		t.Errorf("got %q, want %q", id, "global")
	}
}

func TestConfigRouterWildcardGroup(t *testing.T) {
	r := NewConfigRouter("main", []RoutingRule{
		{Channel: "discord", GroupID: "*", AgentID: "discord-bot"},
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{
		ChannelName: "discord",
		GroupID:     "any-group",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "discord-bot" {
		t.Errorf("got %q, want %q", id, "discord-bot")
	}
}

func TestConfigRouterNoMatch(t *testing.T) {
	r := NewConfigRouter("main", []RoutingRule{
		{Channel: "slack", GroupID: "C123", AgentID: "support"},
	})
	id, err := r.Route(context.Background(), domain.InboundMessage{
		ChannelName: "telegram",
		GroupID:     "G789",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "main" {
		t.Errorf("no match should fall back to default, got %q", id)
	}
}

func TestConfigRouterPriority(t *testing.T) {
	r := NewConfigRouter("main", []RoutingRule{
		{Channel: "slack", GroupID: "C123", AgentID: "specific"},
		{Channel: "slack", GroupID: "*", AgentID: "general"},
	})
	// The exact rule should match first.
	id, err := r.Route(context.Background(), domain.InboundMessage{
		ChannelName: "slack",
		GroupID:     "C123",
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if id != "specific" {
		t.Errorf("got %q, want %q (first matching rule wins)", id, "specific")
	}
}
