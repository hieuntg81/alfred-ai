package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"alfred-ai/internal/domain"
)

// TUIChannel implements domain.Channel using a Bubble Tea TUI program.
type TUIChannel struct {
	logger    *slog.Logger
	program   *tea.Program
	privacy   domain.PrivacyController
	onClear   func()
	agentName string
	modelName string
	gen       atomic.Uint64   // current request generation, set by ChatModel via SetGen
	bus       domain.EventBus // optional, nil = no tool event forwarding
}

// NewTUIChannel creates a new TUI-based CLI channel.
func NewTUIChannel(logger *slog.Logger) *TUIChannel {
	return &TUIChannel{
		logger: logger,
	}
}

// SetPrivacy injects a privacy controller for /privacy, /export, /delete commands.
func (c *TUIChannel) SetPrivacy(pc domain.PrivacyController) {
	c.privacy = pc
}

// SetOnClear registers a callback invoked when the user runs /clear.
func (c *TUIChannel) SetOnClear(fn func()) {
	c.onClear = fn
}

// SetAgentInfo sets the agent name and model name for display.
func (c *TUIChannel) SetAgentInfo(agent, model string) {
	c.agentName = agent
	c.modelName = model
}

// SetEventBus enables forwarding tool events from the EventBus to the TUI.
func (c *TUIChannel) SetEventBus(bus domain.EventBus) {
	c.bus = bus
}

// Start creates the Bubble Tea program and blocks until it exits.
func (c *TUIChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	model := NewChatModel(ChatModelDeps{
		Handler:   handler,
		Privacy:   c.privacy,
		OnClear:   c.onClear,
		OnGenBump: c.SetGen,
		Logger:    c.logger,
		AgentName: c.agentName,
		ModelName: c.modelName,
	})

	c.program = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Forward tool events from EventBus to the TUI program.
	if c.bus != nil {
		unsub1 := c.bus.Subscribe(domain.EventToolCallStarted, func(_ context.Context, event domain.Event) {
			name := extractToolName(event.Payload)
			c.program.Send(ToolStartedMsg{Name: name})
		})
		unsub2 := c.bus.Subscribe(domain.EventToolCallCompleted, func(_ context.Context, event domain.Event) {
			payload := extractToolPayload(event.Payload)
			c.program.Send(ToolCompletedMsg{
				Name:    payload["tool"],
				IsError: payload["success"] == "false",
			})
		})
		defer unsub1()
		defer unsub2()
	}

	// Monitor context cancellation to quit the program.
	go func() {
		<-ctx.Done()
		if c.program != nil {
			c.program.Send(QuitMsg{})
		}
	}()

	_, err := c.program.Run()
	return err
}

// Stop signals the Bubble Tea program to quit.
func (c *TUIChannel) Stop(_ context.Context) error {
	if c.program != nil {
		c.program.Send(QuitMsg{})
	}
	return nil
}

// SetGen updates the current request generation. Called by ChatModel when
// a new request is submitted so Send() can tag outbound messages.
func (c *TUIChannel) SetGen(gen uint64) {
	c.gen.Store(gen)
}

// Send pushes an outbound message into the Bubble Tea update loop.
// Called from the Router goroutine - this is the bridge between
// the domain layer and the TUI. The current gen is tagged so the UI
// can discard responses from cancelled requests.
func (c *TUIChannel) Send(_ context.Context, msg domain.OutboundMessage) error {
	if c.program != nil {
		c.program.Send(OutboundMsg{Message: msg, Gen: c.gen.Load()})
	}
	return nil
}

// Name implements domain.Channel. Returns "cli" to maintain compatibility
// with session key format (cli:cli-default) and config.yaml channel type.
func (c *TUIChannel) Name() string { return "cli" }

// extractToolPayload unmarshals a JSON payload into a string map.
func extractToolPayload(raw json.RawMessage) map[string]string {
	var m map[string]string
	if raw != nil {
		_ = json.Unmarshal(raw, &m)
	}
	if m == nil {
		m = map[string]string{}
	}
	return m
}

// extractToolName extracts the "tool" key from a JSON payload.
func extractToolName(raw json.RawMessage) string {
	p := extractToolPayload(raw)
	if name := p["tool"]; name != "" {
		return name
	}
	return "unknown"
}
