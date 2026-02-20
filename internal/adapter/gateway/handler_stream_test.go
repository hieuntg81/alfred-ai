package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase"
)

// handlerStreamingLLM implements StreamingLLMProvider for handler tests.
type handlerStreamingLLM struct {
	resp string
}

func (s *handlerStreamingLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: s.resp, Timestamp: time.Now()},
	}, nil
}

func (s *handlerStreamingLLM) Name() string { return "handler-streaming-stub" }

func (s *handlerStreamingLLM) ChatStream(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	ch := make(chan domain.StreamDelta, 3)
	ch <- domain.StreamDelta{Content: s.resp}
	ch <- domain.StreamDelta{Done: true}
	close(ch)
	return ch, nil
}

func newStreamHandlerDeps(t *testing.T) HandlerDeps {
	t.Helper()
	bus := &testBus{}
	mem := &handlerStubMemory{}

	agent := usecase.NewAgent(usecase.AgentDeps{
		LLM:            &handlerStreamingLLM{resp: "streamed response"},
		Memory:         mem,
		Tools:          handlerStubTools{},
		ContextBuilder: usecase.NewContextBuilder("test", "model", 50),
		Logger:         slog.Default(),
		MaxIterations:  5,
		Bus:            bus,
	})
	sessions := usecase.NewSessionManager(t.TempDir())
	router := usecase.NewRouter(agent, sessions, bus, slog.Default())

	return HandlerDeps{
		Router:         router,
		Sessions:       sessions,
		Tools:          handlerStubTools{},
		Memory:         mem,
		Bus:            bus,
		Logger:         slog.Default(),
		ActiveRequests: &sync.Map{},
	}
}

func TestHandlerChatStreamImmediateResponse(t *testing.T) {
	deps := newStreamHandlerDeps(t)
	h := chatStreamHandler(deps)

	result, err := callHandler(t, h, `{"session_id":"s1","content":"hi"}`)
	require.NoError(t, err)

	var resp chatStreamResponse
	require.NoError(t, json.Unmarshal(result, &resp))
	assert.True(t, resp.Streaming)
	assert.Equal(t, "s1", resp.SessionID)

	// Wait for background goroutine to complete.
	time.Sleep(100 * time.Millisecond)
}

func TestHandlerChatStreamInvalidPayload(t *testing.T) {
	deps := newStreamHandlerDeps(t)
	h := chatStreamHandler(deps)

	_, err := callHandler(t, h, `invalid json`)
	assert.Error(t, err)
}

func TestHandlerChatStreamMissingFields(t *testing.T) {
	deps := newStreamHandlerDeps(t)
	h := chatStreamHandler(deps)

	_, err := callHandler(t, h, `{"session_id":"","content":""}`)
	assert.Error(t, err)
}

func TestHandlerChatStreamTracksActiveRequest(t *testing.T) {
	deps := newStreamHandlerDeps(t)
	h := chatStreamHandler(deps)

	result, err := callHandler(t, h, `{"session_id":"s2","content":"hi"}`)
	require.NoError(t, err)

	var resp chatStreamResponse
	require.NoError(t, json.Unmarshal(result, &resp))
	assert.True(t, resp.Streaming)

	// Wait for goroutine to finish and clean up.
	time.Sleep(200 * time.Millisecond)

	// After goroutine completes, active request should be cleaned up.
	_, loaded := deps.ActiveRequests.Load("s2")
	assert.False(t, loaded, "active request should be cleaned up after completion")
}
