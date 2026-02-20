package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/sony/gobreaker/v2"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// Default circuit breaker settings.
const (
	defaultCBMaxFailures uint32        = 5
	defaultCBTimeout     time.Duration = 30 * time.Second
	defaultCBInterval    time.Duration = 60 * time.Second
)

// CircuitBreakerConfig configures the circuit breaker behavior.
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures before the circuit opens.
	MaxFailures uint32 `yaml:"max_failures"`
	// Timeout is how long the circuit stays open before transitioning to half-open.
	Timeout time.Duration `yaml:"timeout"`
	// Interval is the cyclic period of the closed state for clearing failure counts.
	// If 0, failures never reset until the circuit opens.
	Interval time.Duration `yaml:"interval"`
}

// CircuitBreakerProvider wraps an LLMProvider with circuit breaker protection.
// When the wrapped provider fails repeatedly, the circuit opens and subsequent
// calls fail fast without reaching the provider, preventing retry storms.
type CircuitBreakerProvider struct {
	inner   domain.LLMProvider
	breaker *gobreaker.CircuitBreaker[*domain.ChatResponse]
	logger  *slog.Logger
}

// NewCircuitBreakerProvider wraps inner with a circuit breaker.
// If cfg is nil or zero-valued, sensible defaults are used.
func NewCircuitBreakerProvider(inner domain.LLMProvider, cfg CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerProvider {
	maxFailures := cfg.MaxFailures
	if maxFailures == 0 {
		maxFailures = defaultCBMaxFailures
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultCBTimeout
	}
	interval := cfg.Interval
	if interval == 0 {
		interval = defaultCBInterval
	}

	name := inner.Name()
	cb := gobreaker.NewCircuitBreaker[*domain.ChatResponse](gobreaker.Settings{
		Name:        "llm:" + name,
		MaxRequests: 1, // allow 1 probe in half-open state
		Interval:    interval,
		Timeout:     timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= maxFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				"breaker", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
		IsSuccessful: func(err error) bool {
			return err == nil
		},
	})

	return &CircuitBreakerProvider{
		inner:   inner,
		breaker: cb,
		logger:  logger,
	}
}

// Chat implements domain.LLMProvider. Calls are routed through the circuit breaker.
func (p *CircuitBreakerProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	resp, err := p.breaker.Execute(func() (*domain.ChatResponse, error) {
		return p.inner.Chat(ctx, req)
	})
	if err != nil {
		// Wrap circuit breaker errors with provider context.
		if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
			return nil, fmt.Errorf("provider %q circuit open: %w", p.inner.Name(), err)
		}
		return nil, err
	}
	return resp, nil
}

// ChatStream implements domain.StreamingLLMProvider if the inner provider supports it.
// The circuit breaker protects the initial connection; streaming errors after connection
// do not trip the breaker (they are returned through the channel).
func (p *CircuitBreakerProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	sp, ok := p.inner.(domain.StreamingLLMProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support streaming", p.inner.Name())
	}

	// Use a zero-result breaker for stream initiation.
	var ch <-chan domain.StreamDelta
	_, err := p.breaker.Execute(func() (*domain.ChatResponse, error) {
		var streamErr error
		ch, streamErr = sp.ChatStream(ctx, req)
		return nil, streamErr
	})
	if err != nil {
		if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
			return nil, fmt.Errorf("provider %q circuit open: %w", p.inner.Name(), err)
		}
		return nil, err
	}
	return ch, nil
}

// Name implements domain.LLMProvider.
func (p *CircuitBreakerProvider) Name() string { return p.inner.Name() }

// State returns the current circuit breaker state for monitoring.
func (p *CircuitBreakerProvider) State() gobreaker.State {
	return p.breaker.State()
}

// Counts returns the current circuit breaker failure/success counts.
func (p *CircuitBreakerProvider) Counts() gobreaker.Counts {
	return p.breaker.Counts()
}

// Compile-time interface checks.
var (
	_ domain.LLMProvider          = (*CircuitBreakerProvider)(nil)
	_ domain.StreamingLLMProvider = (*CircuitBreakerProvider)(nil)
)

// --- Connection Pooling ---

// PooledTransportConfig configures HTTP connection pooling for LLM providers.
type PooledTransportConfig struct {
	MaxIdleConns        int           `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost     int           `yaml:"max_conns_per_host"`
	IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout"`
}

// Default connection pool settings optimized for LLM API usage patterns:
// few hosts, high concurrency, long-lived connections.
const (
	defaultMaxIdleConns        = 20
	defaultMaxIdleConnsPerHost = 10
	defaultMaxConnsPerHost     = 20
	defaultIdleConnTimeout     = 120 * time.Second
)

// NewPooledTransport creates an http.Transport with connection pooling
// optimized for LLM API calls. It accepts per-connection timeouts and
// pool sizing configuration.
func NewPooledTransport(connTimeout, respTimeout time.Duration, pool PooledTransportConfig) *http.Transport {
	if connTimeout == 0 {
		connTimeout = 30 * time.Second
	}
	if respTimeout == 0 {
		respTimeout = 120 * time.Second
	}

	maxIdle := pool.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = defaultMaxIdleConns
	}
	maxIdlePerHost := pool.MaxIdleConnsPerHost
	if maxIdlePerHost <= 0 {
		maxIdlePerHost = defaultMaxIdleConnsPerHost
	}
	maxConnsPerHost := pool.MaxConnsPerHost
	if maxConnsPerHost <= 0 {
		maxConnsPerHost = defaultMaxConnsPerHost
	}
	idleTimeout := pool.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleConnTimeout
	}

	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: respTimeout,
		MaxIdleConns:           maxIdle,
		MaxIdleConnsPerHost:    maxIdlePerHost,
		MaxConnsPerHost:        maxConnsPerHost,
		IdleConnTimeout:        idleTimeout,
		ForceAttemptHTTP2:      true,
	}
}

// Default provider timeouts.
const (
	defaultConnTimeout = 30 * time.Second
	defaultRespTimeout = 120 * time.Second
)

// NewHTTPClient creates an *http.Client with pooled transport and timeout
// defaults suitable for LLM providers. Used by OpenAI, Anthropic, Gemini,
// OpenRouter, and Ollama to avoid duplicating client setup logic.
func NewHTTPClient(cfg config.ProviderConfig) *http.Client {
	connTimeout := cfg.ConnTimeout
	if connTimeout == 0 {
		connTimeout = defaultConnTimeout
	}
	respTimeout := cfg.RespTimeout
	if respTimeout == 0 {
		respTimeout = defaultRespTimeout
	}

	return &http.Client{
		Transport: NewPooledTransport(connTimeout, respTimeout, PooledTransportConfig{
			MaxIdleConns:        cfg.Pool.MaxIdleConns,
			MaxIdleConnsPerHost: cfg.Pool.MaxIdleConnsPerHost,
			MaxConnsPerHost:     cfg.Pool.MaxConnsPerHost,
			IdleConnTimeout:     cfg.Pool.IdleConnTimeout,
		}),
		Timeout: connTimeout + respTimeout,
	}
}
