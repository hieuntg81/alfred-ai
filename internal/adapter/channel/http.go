package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/middleware"
)

// HTTPChannel implements domain.Channel for an HTTP API.
type HTTPChannel struct {
	server  *http.Server
	logger  *slog.Logger
	addr    string
	handler domain.MessageHandler

	// Actual bound address (set after Start)
	boundAddr string

	// Per-request response delivery
	mu       sync.Mutex
	pending  map[string]chan string
	sendFunc func(ctx context.Context, msg domain.OutboundMessage) error

	// Lifecycle management for rate limiter cleanup goroutine
	ctx    context.Context
	cancel context.CancelFunc
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

type chatResponse struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
}

// NewHTTPChannel creates an HTTP API channel.
func NewHTTPChannel(addr string, logger *slog.Logger) *HTTPChannel {
	h := &HTTPChannel{
		addr:    addr,
		logger:  logger,
		pending: make(map[string]chan string),
	}
	return h
}

// Start begins the HTTP server. Non-blocking (starts in goroutine).
func (h *HTTPChannel) Start(ctx context.Context, handler domain.MessageHandler) error {
	h.handler = handler

	// Create cancellable context for rate limiter lifecycle management
	h.ctx, h.cancel = context.WithCancel(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/chat", h.handleChat)
	mux.HandleFunc("/api/v1/health", h.handleHealth)

	// Apply security middleware: headers + rate limiting
	// Rate limit: 100 requests/minute with burst of 20
	// Pass context to rate limiter for cleanup goroutine lifecycle
	secureHandler := middleware.SecurityHeaders(
		middleware.RateLimit(h.ctx, 100, 20)(mux),
	)

	h.server = &http.Server{
		Addr:              h.addr,
		Handler:           secureHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", h.addr, err)
	}
	h.boundAddr = ln.Addr().String()

	go func() {
		h.logger.Info("http channel started", "addr", h.boundAddr)
		if err := h.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			h.logger.Error("http server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (h *HTTPChannel) Stop(ctx context.Context) error {
	// Cancel context to stop rate limiter cleanup goroutine
	if h.cancel != nil {
		h.cancel()
	}

	if h.server == nil {
		return nil
	}
	return h.server.Shutdown(ctx)
}

// Send delivers a response to a pending request.
func (h *HTTPChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	h.mu.Lock()
	ch, ok := h.pending[msg.SessionID]
	h.mu.Unlock()

	if !ok {
		return domain.NewDomainError(
			"HTTPChannel.Send",
			domain.ErrSessionNotFound,
			msg.SessionID,
		)
	}

	select {
	case ch <- msg.Content:
		return nil
	case <-ctx.Done():
		return domain.NewDomainError(
			"HTTPChannel.Send",
			ctx.Err(),
			fmt.Sprintf("context cancelled for session %s", msg.SessionID),
		)
	case <-time.After(5 * time.Second):
		return domain.NewDomainError(
			"HTTPChannel.Send",
			fmt.Errorf("timeout"),
			fmt.Sprintf("timeout sending to session %s", msg.SessionID),
		)
	}
}

// Name implements domain.Channel.
func (h *HTTPChannel) Name() string { return "http" }

func (h *HTTPChannel) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Limit request body to 1MB to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)

		// Provide better error message for oversized requests
		errMsg := "invalid JSON: " + err.Error()
		if err.Error() == "http: request body too large" {
			errMsg = "request body too large (max 1MB)"
		}

		json.NewEncoder(w).Encode(chatResponse{Error: errMsg})
		return
	}

	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("http-%d", time.Now().UnixNano())
	}
	if req.Content == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(chatResponse{Error: "content is required"})
		return
	}

	// Create response channel for this request
	respCh := make(chan string, 1)
	h.mu.Lock()
	h.pending[req.SessionID] = respCh
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, req.SessionID)
		h.mu.Unlock()
	}()

	msg := domain.InboundMessage{
		SessionID:   req.SessionID,
		Content:     req.Content,
		ChannelName: "http",
	}

	if err := h.handler(r.Context(), msg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(chatResponse{
			SessionID: req.SessionID,
			Error:     err.Error(),
		})
		return
	}

	// Wait for response
	select {
	case content := <-respCh:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse{
			SessionID: req.SessionID,
			Content:   content,
		})
	case <-r.Context().Done():
		http.Error(w, `{"error":"request cancelled"}`, http.StatusRequestTimeout)
	}
}

func (h *HTTPChannel) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
