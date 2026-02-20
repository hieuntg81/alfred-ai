package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// Voice call tool constants.
const (
	defaultVoiceCallTimeout           = 30 * time.Second
	defaultVoiceCallMaxDuration       = 5 * time.Minute
	defaultVoiceCallTranscriptTimeout = 3 * time.Minute
	defaultVoiceCallMaxConcurrent     = 1
	defaultVoiceCallMode              = "notify"
)

// e164Re validates E.164 phone number format.
var e164Re = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// VoiceCallToolConfig holds configuration for the VoiceCallTool.
type VoiceCallToolConfig struct {
	FromNumber        string
	DefaultTo         string
	DefaultMode       string
	MaxConcurrent     int
	MaxDuration       time.Duration
	TranscriptTimeout time.Duration
	Timeout           time.Duration
	AllowedNumbers    []string
	WebhookPublicURL  string
	WebhookPath       string
	StreamPath        string
}

// VoiceCallTool provides voice calling capabilities via telephony providers.
type VoiceCallTool struct {
	backend  VoiceCallBackend
	store    *CallStore
	config   VoiceCallToolConfig
	logger   *slog.Logger
}

// NewVoiceCallTool creates a voice call tool backed by the given VoiceCallBackend.
func NewVoiceCallTool(
	backend VoiceCallBackend,
	store *CallStore,
	cfg VoiceCallToolConfig,
	logger *slog.Logger,
) *VoiceCallTool {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultVoiceCallTimeout
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = defaultVoiceCallMaxDuration
	}
	if cfg.TranscriptTimeout <= 0 {
		cfg.TranscriptTimeout = defaultVoiceCallTranscriptTimeout
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = defaultVoiceCallMaxConcurrent
	}
	if cfg.DefaultMode == "" {
		cfg.DefaultMode = defaultVoiceCallMode
	}
	return &VoiceCallTool{
		backend: backend,
		store:   store,
		config:  cfg,
		logger:  logger,
	}
}

func (t *VoiceCallTool) Name() string { return "voice_call" }
func (t *VoiceCallTool) Description() string {
	return "Make outbound voice calls to phone numbers. " +
		"Supports notify mode (one-way message) and conversation mode (bidirectional audio with speech-to-text). " +
		"Use initiate_call to start, continue_call to listen for responses, speak_to_user to send audio, " +
		"end_call to hang up, and get_status to check call state."
}

func (t *VoiceCallTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["initiate_call", "continue_call", "speak_to_user", "end_call", "get_status"],
					"description": "The voice call action to perform"
				},
				"to": {
					"type": "string",
					"description": "Phone number in E.164 format (e.g. +14155551234). Required for initiate_call if no default is configured."
				},
				"message": {
					"type": "string",
					"description": "Message to speak to the user (required for initiate_call and speak_to_user)"
				},
				"call_id": {
					"type": "string",
					"description": "Call ID (required for continue_call, speak_to_user, end_call, get_status)"
				},
				"mode": {
					"type": "string",
					"enum": ["notify", "conversation"],
					"description": "Call mode: notify (one-way message then hangup) or conversation (bidirectional audio)"
				}
			},
			"required": ["action"]
		}`),
	}
}

type voiceCallParams struct {
	Action  string `json:"action"`
	To      string `json:"to,omitempty"`
	Message string `json:"message,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

func (t *VoiceCallTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	ctx, span := tracer.StartSpan(ctx, "tool.voice_call",
		trace.WithAttributes(tracer.StringAttr("tool.name", t.Name())),
	)
	defer span.End()

	p, errResult := ParseParams[voiceCallParams](params)
	if errResult != nil {
		return errResult, nil
	}

	span.SetAttributes(tracer.StringAttr("tool.action", p.Action))

	var result any
	var err error

	switch p.Action {
	case "initiate_call":
		result, err = t.handleInitiateCall(ctx, p)
	case "continue_call":
		result, err = t.handleContinueCall(ctx, p)
	case "speak_to_user":
		result, err = t.handleSpeakToUser(ctx, p)
	case "end_call":
		result, err = t.handleEndCall(ctx, p)
	case "get_status":
		result, err = t.handleGetStatus(ctx, p)
	default:
		return &domain.ToolResult{
			IsError: true,
			Content: fmt.Sprintf("unknown action %q (want: initiate_call, continue_call, speak_to_user, end_call, get_status)", p.Action),
		}, nil
	}

	if err != nil {
		tracer.RecordError(span, err)
		t.logger.Warn("voice call action failed", "action", p.Action, "error", err)

		// Build structured error response.
		errResp := map[string]any{
			"error": err.Error(),
		}
		if p.CallID != "" {
			errResp["call_id"] = p.CallID
			// Try to include state info.
			if call, getErr := t.store.Get(p.CallID); getErr == nil {
				errResp["state"] = call.State
			}
		}

		data, _ := json.MarshalIndent(errResp, "", "  ")
		return &domain.ToolResult{IsError: true, Content: string(data)}, nil
	}

	data, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		tracer.RecordError(span, marshalErr)
		return &domain.ToolResult{IsError: true, Content: fmt.Sprintf("failed to format response: %v", marshalErr)}, nil
	}
	tracer.SetOK(span)
	return &domain.ToolResult{Content: string(data)}, nil
}

func (t *VoiceCallTool) handleInitiateCall(ctx context.Context, p voiceCallParams) (any, error) {
	if err := RequireField("message", p.Message); err != nil {
		return nil, err
	}

	// Resolve phone number.
	to := p.To
	if to == "" {
		to = t.config.DefaultTo
	}
	if to == "" {
		return nil, fmt.Errorf("'to' phone number is required (no default configured)")
	}
	if !e164Re.MatchString(to) {
		return nil, fmt.Errorf("%w: %q (expected E.164 format like +14155551234)", domain.ErrInvalidInput, to)
	}

	// Check allowlist.
	if len(t.config.AllowedNumbers) > 0 {
		allowed := false
		for _, n := range t.config.AllowedNumbers {
			if n == to {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("phone number %q is not in the allowed numbers list", to)
		}
	}

	// Resolve mode.
	mode := p.Mode
	if mode == "" {
		mode = t.config.DefaultMode
	}
	if err := ValidateAll(RequireField("mode", mode), ValidateEnum("mode", mode, "notify", "conversation")); err != nil {
		return nil, err
	}

	// Generate call ID.
	callID := generateCallID()

	// Create call record.
	record := CallRecord{
		ID:      callID,
		To:      to,
		From:    t.config.FromNumber,
		Mode:    mode,
		State:   CallStateInitiated,
		Message: p.Message,
	}

	if err := t.store.Create(record); err != nil {
		return nil, err
	}

	// Build webhook and stream URLs.
	webhookURL := t.config.WebhookPublicURL + t.config.WebhookPath
	streamURL := ""
	if mode == "conversation" {
		streamURL = t.config.WebhookPublicURL + t.config.StreamPath + "?call_id=" + callID
	}

	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()

	// Initiate call via backend.
	resp, err := t.backend.InitiateCall(ctx, InitiateCallRequest{
		To:          to,
		From:        t.config.FromNumber,
		Message:     p.Message,
		Mode:        mode,
		WebhookURL:  webhookURL,
		StreamURL:   streamURL,
		CallID:      callID,
		MaxDuration: int(t.config.MaxDuration.Seconds()),
	})
	if err != nil {
		_ = t.store.Transition(callID, CallStateFailed, err.Error())
		return nil, fmt.Errorf("%w: %v", domain.ErrProviderError, err)
	}

	// Set provider call ID.
	_ = t.store.SetProviderCallID(callID, resp.ProviderCallID)

	t.logger.Info("voice call initiated",
		"call_id", callID,
		"to", to,
		"mode", mode,
		"provider_call_id", resp.ProviderCallID,
	)

	return map[string]any{
		"call_id": callID,
		"status":  string(CallStateInitiated),
		"to":      to,
		"mode":    mode,
	}, nil
}

func (t *VoiceCallTool) handleContinueCall(_ context.Context, p voiceCallParams) (any, error) {
	if err := RequireFields("call_id", p.CallID, "message", p.Message); err != nil {
		return nil, err
	}

	call, err := t.store.Get(p.CallID)
	if err != nil {
		return nil, err
	}

	if call.State.IsTerminal() {
		return map[string]any{
			"call_id":    p.CallID,
			"state":      string(call.State),
			"transcript": call.Transcript,
			"ended":      true,
		}, nil
	}

	// Append bot message to transcript.
	_ = t.store.AppendTranscript(p.CallID, TurnEntry{
		Role: "bot",
		Text: p.Message,
	})

	// Wait for user response (transcript update).
	entries, err := t.store.WaitForTranscript(p.CallID, len(call.Transcript)+1, t.config.TranscriptTimeout)
	if err != nil {
		return nil, err
	}

	// Get updated call state.
	updated, err := t.store.Get(p.CallID)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"call_id": p.CallID,
		"state":   string(updated.State),
	}

	if len(entries) > 0 {
		result["transcript"] = entries
	} else {
		result["transcript"] = "no response received within timeout"
	}

	if updated.State.IsTerminal() {
		result["ended"] = true
	}

	return result, nil
}

func (t *VoiceCallTool) handleSpeakToUser(ctx context.Context, p voiceCallParams) (any, error) {
	if err := RequireFields("call_id", p.CallID, "message", p.Message); err != nil {
		return nil, err
	}

	call, err := t.store.Get(p.CallID)
	if err != nil {
		return nil, err
	}

	if call.State.IsTerminal() {
		return nil, fmt.Errorf("%w: call is in state %s", domain.ErrInvalidInput, call.State)
	}

	// Append to transcript.
	_ = t.store.AppendTranscript(p.CallID, TurnEntry{
		Role: "bot",
		Text: p.Message,
	})

	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()

	// Play TTS via backend.
	if err := t.backend.PlayTTS(ctx, PlayTTSRequest{
		ProviderCallID: call.ProviderCallID,
		Message:        p.Message,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrProviderError, err)
	}

	return map[string]any{
		"success": true,
		"call_id": p.CallID,
	}, nil
}

func (t *VoiceCallTool) handleEndCall(ctx context.Context, p voiceCallParams) (any, error) {
	if err := RequireField("call_id", p.CallID); err != nil {
		return nil, err
	}

	call, err := t.store.Get(p.CallID)
	if err != nil {
		return nil, err
	}

	if call.State.IsTerminal() {
		return map[string]any{
			"success": true,
			"call_id": p.CallID,
			"state":   string(call.State),
			"note":    "call was already ended",
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()

	// Hang up via backend.
	if call.ProviderCallID != "" {
		if err := t.backend.HangupCall(ctx, HangupCallRequest{
			ProviderCallID: call.ProviderCallID,
		}); err != nil {
			t.logger.Warn("hangup backend error", "call_id", p.CallID, "error", err)
		}
	}

	_ = t.store.Transition(p.CallID, CallStateHangupBot, "ended by bot")

	return map[string]any{
		"success": true,
		"call_id": p.CallID,
		"state":   string(CallStateHangupBot),
	}, nil
}

func (t *VoiceCallTool) handleGetStatus(_ context.Context, p voiceCallParams) (any, error) {
	if err := RequireField("call_id", p.CallID); err != nil {
		return nil, err
	}

	call, err := t.store.Get(p.CallID)
	if err != nil {
		return nil, err
	}

	return call, nil
}

// generateCallID creates a unique call ID using timestamp + random suffix.
func generateCallID() string {
	return fmt.Sprintf("vc_%d", time.Now().UnixNano())
}

// HangupActiveCalls terminates all non-terminal calls (used for graceful shutdown).
func (t *VoiceCallTool) HangupActiveCalls(ctx context.Context) {
	active := t.store.ActiveCalls()
	for _, call := range active {
		if call.ProviderCallID != "" {
			_ = t.backend.HangupCall(ctx, HangupCallRequest{
				ProviderCallID: call.ProviderCallID,
			})
		}
		_ = t.store.Transition(call.ID, CallStateHangupBot, "server shutdown")
	}
	if len(active) > 0 {
		t.logger.Info("hung up active calls on shutdown", "count", len(active))
	}
}
