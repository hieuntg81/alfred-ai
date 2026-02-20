package usecase

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

func newStreamRouter(llm domain.LLMProvider) (*Router, *recordingBus) {
	bus := &recordingBus{}
	agent := NewAgent(AgentDeps{
		LLM:            llm,
		Memory:         &mockMemory{},
		Tools:          &mockToolExecutor{tools: map[string]domain.Tool{}},
		ContextBuilder: NewContextBuilder("test", "model", 50),
		Logger:         newTestLogger(),
		MaxIterations:  10,
		Bus:            bus,
	})
	sessions := NewSessionManager(os.TempDir())
	router := NewRouter(agent, sessions, bus, newTestLogger())
	return router, bus
}

func TestRouterHandleStream(t *testing.T) {
	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			{
				{Content: "Streaming "},
				{Content: "response", Done: true},
			},
		},
	}

	router, bus := newStreamRouter(llm)

	out, err := router.HandleStream(context.Background(), domain.InboundMessage{
		SessionID:   "s1",
		Content:     "Hi",
		ChannelName: "test",
		SenderName:  "user",
	})
	require.NoError(t, err)
	assert.Equal(t, "s1", out.SessionID)
	assert.Contains(t, out.Content, "Streaming response")

	// Verify streaming events were published.
	events := bus.Events()
	types := make([]domain.EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	assert.Contains(t, types, domain.EventStreamStarted)
	assert.Contains(t, types, domain.EventStreamDelta)
	assert.Contains(t, types, domain.EventStreamCompleted)
	assert.Contains(t, types, domain.EventMessageReceived)
	assert.Contains(t, types, domain.EventMessageSent)
}

func TestRouterHandleStreamHooks(t *testing.T) {
	llm := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			{{Content: "original", Done: true}},
		},
	}

	router, _ := newStreamRouter(llm)
	hook := &spyHook{modifyResp: "modified"}
	router.SetHooks([]domain.PluginHook{hook})

	out, err := router.HandleStream(context.Background(), domain.InboundMessage{
		SessionID:   "s1",
		Content:     "Hi",
		ChannelName: "test",
		SenderName:  "user",
	})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "modified")
	assert.Equal(t, 1, hook.msgReceived)
	assert.Equal(t, 1, hook.responseReady)
}

func TestRouterHandleStreamSameResultAsSync(t *testing.T) {
	// Both streaming and sync should produce the same content for the same LLM response.
	syncLLM := &mockLLM{
		responses: []domain.ChatResponse{
			{Message: domain.Message{Role: domain.RoleAssistant, Content: "Same content"}},
		},
	}
	streamLLM := &mockStreamingLLM{
		streams: [][]domain.StreamDelta{
			{{Content: "Same content", Done: true}},
		},
	}

	syncRouter, _ := newStreamRouter(syncLLM)
	streamRouter, _ := newStreamRouter(streamLLM)

	msg := domain.InboundMessage{
		SessionID:   "s1",
		Content:     "Hi",
		ChannelName: "test",
		SenderName:  "user",
	}

	syncOut, err := syncRouter.Handle(context.Background(), msg)
	require.NoError(t, err)

	streamOut, err := streamRouter.HandleStream(context.Background(), msg)
	require.NoError(t, err)

	// Both should contain "Same content" (may have onboarding prefix).
	assert.Contains(t, syncOut.Content, "Same content")
	assert.Contains(t, streamOut.Content, "Same content")
}
