package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"alfred-ai/internal/domain"
)

// --- Circuit Breaker Tests ---

func TestCircuitBreakerPassesThrough(t *testing.T) {
	inner := &mockProvider{
		name: "test",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return &domain.ChatResponse{Message: domain.Message{Content: "ok"}}, nil
		},
	}

	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{}, slog.Default())
	resp, err := cb.Chat(context.Background(), domain.ChatRequest{})

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Message.Content)
}

func TestCircuitBreakerName(t *testing.T) {
	inner := &mockProvider{name: "openai"}
	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{}, slog.Default())
	assert.Equal(t, "openai", cb.Name())
}

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	callCount := 0
	inner := &mockProvider{
		name: "flaky",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			callCount++
			return nil, errors.New("provider error")
		},
	}

	cfg := CircuitBreakerConfig{
		MaxFailures: 3,
		Timeout:     5 * time.Second,
		Interval:    60 * time.Second,
	}
	cb := NewCircuitBreakerProvider(inner, cfg, slog.Default())

	// First 3 calls go through and fail.
	for i := 0; i < 3; i++ {
		_, err := cb.Chat(context.Background(), domain.ChatRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider error")
	}
	assert.Equal(t, 3, callCount)

	// Circuit should now be open.
	assert.Equal(t, gobreaker.StateOpen, cb.State())

	// Next call should fail fast without reaching the provider.
	_, err := cb.Chat(context.Background(), domain.ChatRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit open")
	assert.Equal(t, 3, callCount, "provider should not be called when circuit is open")
}

func TestCircuitBreakerClosesAfterSuccess(t *testing.T) {
	shouldFail := true
	inner := &mockProvider{
		name: "recovering",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			if shouldFail {
				return nil, errors.New("down")
			}
			return &domain.ChatResponse{Message: domain.Message{Content: "recovered"}}, nil
		},
	}

	cfg := CircuitBreakerConfig{
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond, // short timeout for testing
		Interval:    60 * time.Second,
	}
	cb := NewCircuitBreakerProvider(inner, cfg, slog.Default())

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		cb.Chat(context.Background(), domain.ChatRequest{})
	}
	assert.Equal(t, gobreaker.StateOpen, cb.State())

	// Wait for half-open transition.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, gobreaker.StateHalfOpen, cb.State())

	// Next call should probe (half-open allows 1 request).
	shouldFail = false
	resp, err := cb.Chat(context.Background(), domain.ChatRequest{})
	require.NoError(t, err)
	assert.Equal(t, "recovered", resp.Message.Content)

	// Circuit should be closed again.
	assert.Equal(t, gobreaker.StateClosed, cb.State())
}

func TestCircuitBreakerPropagatesInnerErrors(t *testing.T) {
	sentinel := errors.New("specific error")
	inner := &mockProvider{
		name: "err",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return nil, sentinel
		},
	}

	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{MaxFailures: 10}, slog.Default())
	_, err := cb.Chat(context.Background(), domain.ChatRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestCircuitBreakerStream_Success(t *testing.T) {
	inner := &mockStreamProvider{
		mockProvider: mockProvider{
			name: "stream",
			chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
				return nil, nil
			},
		},
		streamFunc: func(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
			ch := make(chan domain.StreamDelta, 1)
			ch <- domain.StreamDelta{Content: "streamed", Done: true}
			close(ch)
			return ch, nil
		},
	}

	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{}, slog.Default())
	ch, err := cb.ChatStream(context.Background(), domain.ChatRequest{})
	require.NoError(t, err)

	delta := <-ch
	assert.Equal(t, "streamed", delta.Content)
}

func TestCircuitBreakerStream_NonStreamingProvider(t *testing.T) {
	inner := &mockProvider{name: "no-stream"}
	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{}, slog.Default())

	_, err := cb.ChatStream(context.Background(), domain.ChatRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support streaming")
}

func TestCircuitBreakerStream_TripsOnFailure(t *testing.T) {
	inner := &mockStreamProvider{
		mockProvider: mockProvider{
			name: "stream-flaky",
			chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
				return nil, nil
			},
		},
		streamFunc: func(_ context.Context, _ domain.ChatRequest) (<-chan domain.StreamDelta, error) {
			return nil, errors.New("stream init failed")
		},
	}

	cfg := CircuitBreakerConfig{MaxFailures: 2, Timeout: 5 * time.Second}
	cb := NewCircuitBreakerProvider(inner, cfg, slog.Default())

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		cb.ChatStream(context.Background(), domain.ChatRequest{})
	}

	assert.Equal(t, gobreaker.StateOpen, cb.State())

	_, err := cb.ChatStream(context.Background(), domain.ChatRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit open")
}

func TestCircuitBreakerCounts(t *testing.T) {
	callNum := 0
	inner := &mockProvider{
		name: "counted",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			callNum++
			if callNum <= 2 {
				return &domain.ChatResponse{Message: domain.Message{Content: "ok"}}, nil
			}
			return nil, errors.New("fail")
		},
	}

	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{MaxFailures: 10}, slog.Default())

	// 2 successes.
	cb.Chat(context.Background(), domain.ChatRequest{})
	cb.Chat(context.Background(), domain.ChatRequest{})

	counts := cb.Counts()
	assert.Equal(t, uint32(2), counts.TotalSuccesses)

	// 1 failure.
	cb.Chat(context.Background(), domain.ChatRequest{})

	counts = cb.Counts()
	assert.Equal(t, uint32(1), counts.TotalFailures)
	assert.Equal(t, uint32(1), counts.ConsecutiveFailures)
}

func TestCircuitBreakerDefaultConfig(t *testing.T) {
	inner := &mockProvider{
		name: "defaults",
		chatFunc: func(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
			return &domain.ChatResponse{}, nil
		},
	}

	// Zero config should use sensible defaults, not panic.
	cb := NewCircuitBreakerProvider(inner, CircuitBreakerConfig{}, slog.Default())
	resp, err := cb.Chat(context.Background(), domain.ChatRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// --- Connection Pooling Tests ---

func TestNewPooledTransport_Defaults(t *testing.T) {
	tr := NewPooledTransport(0, 0, PooledTransportConfig{})

	assert.Equal(t, defaultMaxIdleConns, tr.MaxIdleConns)
	assert.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
	assert.Equal(t, defaultMaxConnsPerHost, tr.MaxConnsPerHost)
	assert.Equal(t, defaultIdleConnTimeout, tr.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)
	assert.True(t, tr.ForceAttemptHTTP2)
}

func TestNewPooledTransport_CustomConfig(t *testing.T) {
	cfg := PooledTransportConfig{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 25,
		MaxConnsPerHost:     30,
		IdleConnTimeout:     5 * time.Minute,
	}
	tr := NewPooledTransport(15*time.Second, 60*time.Second, cfg)

	assert.Equal(t, 50, tr.MaxIdleConns)
	assert.Equal(t, 25, tr.MaxIdleConnsPerHost)
	assert.Equal(t, 30, tr.MaxConnsPerHost)
	assert.Equal(t, 5*time.Minute, tr.IdleConnTimeout)
	assert.Equal(t, 60*time.Second, tr.ResponseHeaderTimeout)
}

func TestNewPooledTransport_IsHTTPTransport(t *testing.T) {
	tr := NewPooledTransport(0, 0, PooledTransportConfig{})

	// Verify it can be used in an http.Client.
	client := &http.Client{Transport: tr}
	require.NotNil(t, client)
}
