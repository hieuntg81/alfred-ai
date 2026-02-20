package tool

import (
	"context"
	"time"
)

// VoiceCallBackend abstracts telephony provider operations.
type VoiceCallBackend interface {
	// InitiateCall places an outbound call.
	InitiateCall(ctx context.Context, req InitiateCallRequest) (*InitiateCallResponse, error)
	// HangupCall terminates an active call.
	HangupCall(ctx context.Context, req HangupCallRequest) error
	// PlayTTS speaks text into the call (used in notify mode).
	PlayTTS(ctx context.Context, req PlayTTSRequest) error
	// VerifyWebhook validates the authenticity of a webhook request.
	VerifyWebhook(ctx context.Context, req WebhookVerifyRequest) error
	// ParseWebhookEvent parses a provider webhook into normalized events.
	ParseWebhookEvent(ctx context.Context, req WebhookParseRequest) ([]CallEvent, *WebhookResponse, error)
	// Name returns the backend identifier (e.g. "twilio", "mock").
	Name() string
}

// TTSProvider abstracts text-to-speech synthesis.
type TTSProvider interface {
	// SynthesizeStream returns a channel of PCM audio chunks (streaming).
	// The channel is closed when synthesis is complete.
	SynthesizeStream(ctx context.Context, req TTSSynthesizeRequest) (<-chan TTSAudioChunk, error)
	// Name returns the provider identifier.
	Name() string
}

// STTProvider abstracts speech-to-text transcription.
type STTProvider interface {
	// StartSession opens a real-time transcription session.
	StartSession(ctx context.Context, cfg STTSessionConfig) (STTSession, error)
	// Name returns the provider identifier.
	Name() string
}

// STTSession represents an active speech-to-text transcription session.
type STTSession interface {
	// SendAudio sends raw audio data (mu-law 8kHz) to the STT engine.
	SendAudio(data []byte) error
	// Transcripts returns a channel of transcription results.
	Transcripts() <-chan STTTranscript
	// Close ends the session.
	Close() error
}

// --- Request / Response DTOs ---

// InitiateCallRequest holds parameters for placing an outbound call.
type InitiateCallRequest struct {
	To             string `json:"to"`               // E.164 phone number
	From           string `json:"from"`              // E.164 phone number
	Message        string `json:"message"`           // message to speak or begin conversation
	Mode           string `json:"mode"`              // "notify" or "conversation"
	WebhookURL     string `json:"webhook_url"`       // status callback URL
	StreamURL      string `json:"stream_url"`        // WebSocket media stream URL
	CallID         string `json:"call_id"`           // internal call ID
	MaxDuration    int    `json:"max_duration_secs"`  // max call duration in seconds
}

// InitiateCallResponse holds the result of placing an outbound call.
type InitiateCallResponse struct {
	ProviderCallID string `json:"provider_call_id"` // provider's call SID/ID
	Status         string `json:"status"`           // initial status from provider
}

// HangupCallRequest holds parameters for hanging up a call.
type HangupCallRequest struct {
	ProviderCallID string `json:"provider_call_id"`
}

// PlayTTSRequest holds parameters for playing TTS audio into a call.
type PlayTTSRequest struct {
	ProviderCallID string `json:"provider_call_id"`
	Message        string `json:"message"`
	Voice          string `json:"voice,omitempty"`
}

// WebhookVerifyRequest holds data needed to verify a webhook signature.
type WebhookVerifyRequest struct {
	URL       string            `json:"url"`
	Body      []byte            `json:"body"`
	Signature string            `json:"signature"`
	Headers   map[string]string `json:"headers"`
}

// WebhookParseRequest holds the raw webhook data to parse.
type WebhookParseRequest struct {
	Body    []byte            `json:"body"`
	Headers map[string]string `json:"headers"`
	URL     string            `json:"url"`
}

// CallEvent represents a normalized call lifecycle event.
type CallEvent struct {
	CallID         string    `json:"call_id"`
	ProviderCallID string    `json:"provider_call_id"`
	Type           string    `json:"type"`   // "status", "answered", "completed", "failed", etc.
	Status         CallState `json:"status"` // normalized call state
	Timestamp      time.Time `json:"timestamp"`
	Detail         string    `json:"detail,omitempty"`
}

// WebhookResponse holds the response to send back to the provider.
type WebhookResponse struct {
	ContentType string `json:"content_type"` // e.g. "text/xml"
	Body        []byte `json:"body"`
}

// --- TTS/STT DTOs ---

// TTSSynthesizeRequest holds parameters for TTS synthesis.
type TTSSynthesizeRequest struct {
	Text       string `json:"text"`
	Voice      string `json:"voice"`       // e.g. "alloy", "nova"
	SampleRate int    `json:"sample_rate"` // output sample rate (e.g. 24000)
	Model      string `json:"model"`       // e.g. "tts-1", "tts-1-hd"
}

// TTSAudioChunk is a chunk of PCM audio from TTS synthesis.
type TTSAudioChunk struct {
	PCMData []byte // raw PCM audio (16-bit signed LE)
	Err     error  // nil unless synthesis failed
}

// STTSessionConfig holds configuration for an STT session.
type STTSessionConfig struct {
	Language   string `json:"language,omitempty"` // e.g. "en"
	Model      string `json:"model"`             // e.g. "gpt-4o-transcribe"
	SampleRate int    `json:"sample_rate"`        // input sample rate (e.g. 8000)
	Encoding   string `json:"encoding"`           // e.g. "mulaw"
}

// STTTranscript represents a transcription result.
type STTTranscript struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
	Err     error  `json:"-"`
}

// --- Call state types ---

// CallState represents the state of a voice call.
type CallState string

const (
	CallStateInitiated  CallState = "initiated"
	CallStateRinging    CallState = "ringing"
	CallStateAnswered   CallState = "answered"
	CallStateActive     CallState = "active"
	CallStateSpeaking   CallState = "speaking"
	CallStateListening  CallState = "listening"
	CallStateCompleted  CallState = "completed"
	CallStateHangupUser CallState = "hangup_user"
	CallStateHangupBot  CallState = "hangup_bot"
	CallStateTimeout    CallState = "timeout"
	CallStateError      CallState = "error"
	CallStateFailed     CallState = "failed"
	CallStateNoAnswer   CallState = "no_answer"
	CallStateBusy       CallState = "busy"
)

// callStateOrder defines the monotonic ordering for non-terminal states.
var callStateOrder = map[CallState]int{
	CallStateInitiated: 0,
	CallStateRinging:   1,
	CallStateAnswered:  2,
	CallStateActive:    3,
	CallStateSpeaking:  4,
	CallStateListening: 5,
}

// terminalStates are absorbing — once reached, no further transitions are allowed.
var terminalStates = map[CallState]bool{
	CallStateCompleted:  true,
	CallStateHangupUser: true,
	CallStateHangupBot:  true,
	CallStateTimeout:    true,
	CallStateError:      true,
	CallStateFailed:     true,
	CallStateNoAnswer:   true,
	CallStateBusy:       true,
}

// IsTerminal returns true if the state is a terminal (absorbing) state.
func (s CallState) IsTerminal() bool {
	return terminalStates[s]
}

// CanTransitionTo checks whether a transition from s to next is valid.
func (s CallState) CanTransitionTo(next CallState) bool {
	if s.IsTerminal() {
		return false
	}
	// Any non-terminal state can transition to a terminal state.
	if next.IsTerminal() {
		return true
	}
	// speaking ↔ listening can cycle.
	if (s == CallStateSpeaking && next == CallStateListening) ||
		(s == CallStateListening && next == CallStateSpeaking) {
		return true
	}
	// Otherwise, must be monotonically forward.
	cur, curOk := callStateOrder[s]
	nxt, nxtOk := callStateOrder[next]
	if !curOk || !nxtOk {
		return false
	}
	return nxt > cur
}

// CallRecord holds the full state of a voice call.
type CallRecord struct {
	ID             string       `json:"id"`
	ProviderCallID string       `json:"provider_call_id,omitempty"`
	To             string       `json:"to"`
	From           string       `json:"from"`
	Mode           string       `json:"mode"` // "notify" or "conversation"
	State          CallState    `json:"state"`
	Message        string       `json:"message,omitempty"`
	Transcript     []TurnEntry  `json:"transcript,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	EndedAt        *time.Time   `json:"ended_at,omitempty"`
	Duration       int          `json:"duration_ms,omitempty"`
	ErrorDetail    string       `json:"error_detail,omitempty"`
}

// TurnEntry represents a single turn in a voice call transcript.
type TurnEntry struct {
	Role      string    `json:"role"` // "bot" or "user"
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}
