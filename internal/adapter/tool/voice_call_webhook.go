package tool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	webhookMaxBodySize = 1 << 20 // 1 MiB
)

// VoiceCallWebhookConfig holds configuration for the webhook server.
type VoiceCallWebhookConfig struct {
	Addr           string // listen address (e.g. ":3334")
	WebhookPath    string // e.g. "/voice/webhook"
	StreamPath     string // e.g. "/voice/stream"
	PublicURL      string // external URL for callbacks
	SkipVerify     bool   // dev-only: skip signature verification
}

// VoiceCallWebhookServer handles telephony provider callbacks and WebSocket media streams.
type VoiceCallWebhookServer struct {
	config      VoiceCallWebhookConfig
	backend     VoiceCallBackend
	store       *CallStore
	sttProvider STTProvider
	ttsProvider TTSProvider
	logger      *slog.Logger

	httpSrv   *http.Server
	boundAddr string
	mu        sync.Mutex
	streams   map[string]*MediaStreamHandler // callID → handler
}

// NewVoiceCallWebhookServer creates a new webhook server.
func NewVoiceCallWebhookServer(
	cfg VoiceCallWebhookConfig,
	backend VoiceCallBackend,
	store *CallStore,
	sttProvider STTProvider,
	ttsProvider TTSProvider,
	logger *slog.Logger,
) *VoiceCallWebhookServer {
	return &VoiceCallWebhookServer{
		config:      cfg,
		backend:     backend,
		store:       store,
		sttProvider: sttProvider,
		ttsProvider: ttsProvider,
		logger:      logger,
		streams:     make(map[string]*MediaStreamHandler),
	}
}

// Start begins serving webhook and media stream requests.
func (s *VoiceCallWebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.config.WebhookPath, s.handleWebhook)
	mux.HandleFunc(s.config.StreamPath, s.handleStream)

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("voice call webhook listen: %w", err)
	}
	s.boundAddr = listener.Addr().String()

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		s.logger.Info("voice call webhook server started", "addr", s.boundAddr)
		if err := s.httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("voice call webhook server error", "error", err)
		}
	}()

	// Shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		s.Stop(context.Background())
	}()

	return nil
}

// Stop shuts down the webhook server and closes all active media streams.
func (s *VoiceCallWebhookServer) Stop(ctx context.Context) {
	s.mu.Lock()
	for callID, handler := range s.streams {
		handler.Close()
		delete(s.streams, callID)
	}
	s.mu.Unlock()

	if s.httpSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		s.httpSrv.Shutdown(shutdownCtx)
	}
}

// BoundAddr returns the address the server is listening on.
func (s *VoiceCallWebhookServer) BoundAddr() string {
	return s.boundAddr
}

// handleWebhook processes telephony provider callbacks (status updates, etc.).
func (s *VoiceCallWebhookServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Enforce body size limit.
	r.Body = http.MaxBytesReader(w, r.Body, webhookMaxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Collect headers.
	headers := make(map[string]string)
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	// Verify webhook signature (unless skip_verify is set).
	if !s.config.SkipVerify {
		verifyReq := WebhookVerifyRequest{
			URL:       s.config.PublicURL + r.URL.Path,
			Body:      body,
			Signature: r.Header.Get("X-Twilio-Signature"),
			Headers:   headers,
		}
		if err := s.backend.VerifyWebhook(r.Context(), verifyReq); err != nil {
			s.logger.Warn("webhook verification failed", "error", err)
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}
	}

	// Parse webhook events.
	events, resp, err := s.backend.ParseWebhookEvent(r.Context(), WebhookParseRequest{
		Body:    body,
		Headers: headers,
		URL:     r.URL.String(),
	})
	if err != nil {
		s.logger.Warn("webhook parse failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Process events (update call store).
	for _, event := range events {
		s.processCallEvent(event)
	}

	// Send response back to provider.
	if resp != nil {
		w.Header().Set("Content-Type", resp.ContentType)
		w.Write(resp.Body)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// processCallEvent updates the call store based on a normalized event.
func (s *VoiceCallWebhookServer) processCallEvent(event CallEvent) {
	// Try to find call by provider ID first, then by internal ID.
	call, err := s.store.FindByProviderID(event.ProviderCallID)
	if err != nil {
		if event.CallID != "" {
			call, err = s.store.Get(event.CallID)
		}
		if err != nil {
			s.logger.Warn("webhook event for unknown call",
				"provider_call_id", event.ProviderCallID,
				"call_id", event.CallID,
			)
			return
		}
	}

	// Transition state.
	if err := s.store.Transition(call.ID, event.Status, event.Detail); err != nil {
		s.logger.Debug("webhook state transition skipped",
			"call_id", call.ID,
			"from", call.State,
			"to", event.Status,
			"error", err,
		)
	} else {
		s.logger.Info("call state updated",
			"call_id", call.ID,
			"state", event.Status,
			"event_type", event.Type,
		)
	}
}

// handleStream handles WebSocket upgrade for media streams.
func (s *VoiceCallWebhookServer) handleStream(w http.ResponseWriter, r *http.Request) {
	callID := r.URL.Query().Get("call_id")
	if callID == "" {
		http.Error(w, "missing call_id", http.StatusBadRequest)
		return
	}

	// Verify call exists and is active.
	call, err := s.store.Get(callID)
	if err != nil {
		http.Error(w, "call not found", http.StatusNotFound)
		return
	}
	if call.State.IsTerminal() {
		http.Error(w, "call already ended", http.StatusGone)
		return
	}

	// Create media stream handler.
	handler := NewMediaStreamHandler(MediaStreamConfig{
		CallID:      callID,
		STTProvider: s.sttProvider,
		TTSProvider: s.ttsProvider,
		Store:       s.store,
		Logger:      s.logger,
	})

	s.mu.Lock()
	s.streams[callID] = handler
	s.mu.Unlock()

	defer func() {
		handler.Close()
		s.mu.Lock()
		delete(s.streams, callID)
		s.mu.Unlock()
	}()

	// Handle the WebSocket connection (blocks until stream ends).
	handler.HandleHTTP(w, r)
}

// GetMediaHandler returns the active media handler for a call, if any.
func (s *VoiceCallWebhookServer) GetMediaHandler(callID string) *MediaStreamHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[callID]
}
