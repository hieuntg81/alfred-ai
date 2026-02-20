package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- interface compliance ---

var _ domain.Tool = (*VoiceCallTool)(nil)
var _ VoiceCallBackend = (*MockVoiceCallBackend)(nil)
var _ CallPersistence = (*FileCallStore)(nil)

// --- helpers ---

func newTestVoiceCallTool(backend *MockVoiceCallBackend) *VoiceCallTool {
	store := NewCallStore(5, nil)
	return NewVoiceCallTool(backend, store, VoiceCallToolConfig{
		FromNumber:       "+15551234567",
		DefaultTo:        "+15559876543",
		DefaultMode:      "notify",
		MaxConcurrent:    5,
		MaxDuration:      5 * time.Minute,
		TranscriptTimeout: 5 * time.Second,
		Timeout:          5 * time.Second,
		WebhookPublicURL: "https://example.com",
		WebhookPath:      "/voice/webhook",
		StreamPath:       "/voice/stream",
	}, newTestLogger())
}

func execVoiceCall(t *testing.T, vt *VoiceCallTool, params any) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := vt.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

func getCallID(t *testing.T, result *domain.ToolResult) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	callID, ok := resp["call_id"].(string)
	if !ok {
		t.Fatal("missing call_id in response")
	}
	return callID
}

// --- metadata tests ---

func TestVoiceCallToolName(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	if vt.Name() != "voice_call" {
		t.Errorf("Name() = %q, want %q", vt.Name(), "voice_call")
	}
}

func TestVoiceCallToolDescription(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	if vt.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestVoiceCallToolSchema(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	schema := vt.Schema()
	if schema.Name != "voice_call" {
		t.Errorf("Schema.Name = %q, want %q", schema.Name, "voice_call")
	}
	if schema.Parameters == nil {
		t.Error("Schema.Parameters is nil")
	}
	var v any
	if err := json.Unmarshal(schema.Parameters, &v); err != nil {
		t.Errorf("Schema.Parameters is not valid JSON: %v", err)
	}
}

// --- invalid input tests ---

func TestVoiceCallToolInvalidJSON(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result, err := vt.Execute(context.Background(), json.RawMessage(`invalid`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestVoiceCallToolUnknownAction(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]string{"action": "unknown"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

// --- initiate_call tests ---

func TestVoiceCallInitiateSuccess(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello, this is a test call",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	callID := getCallID(t, result)
	if callID == "" {
		t.Fatal("empty call_id")
	}

	if len(mock.InitiateCalls) != 1 {
		t.Fatalf("expected 1 initiate call, got %d", len(mock.InitiateCalls))
	}
	if mock.InitiateCalls[0].To != "+15559876543" {
		t.Errorf("to = %q, want default +15559876543", mock.InitiateCalls[0].To)
	}
	if mock.InitiateCalls[0].Mode != "notify" {
		t.Errorf("mode = %q, want notify", mock.InitiateCalls[0].Mode)
	}
}

func TestVoiceCallInitiateWithExplicitTo(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"to":      "+14155551234",
		"message": "Hello",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if mock.InitiateCalls[0].To != "+14155551234" {
		t.Errorf("to = %q, want +14155551234", mock.InitiateCalls[0].To)
	}
}

func TestVoiceCallInitiateConversationMode(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
		"mode":    "conversation",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if mock.InitiateCalls[0].Mode != "conversation" {
		t.Errorf("mode = %q, want conversation", mock.InitiateCalls[0].Mode)
	}
	if mock.InitiateCalls[0].StreamURL == "" {
		t.Error("expected non-empty stream URL for conversation mode")
	}
}

func TestVoiceCallInitiateMissingMessage(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action": "initiate_call",
	})
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

func TestVoiceCallInitiateInvalidPhone(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"to":      "not-a-phone",
		"message": "Hello",
	})
	if !result.IsError {
		t.Error("expected error for invalid phone number")
	}
}

func TestVoiceCallInitiateInvalidMode(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
		"mode":    "invalid",
	})
	if !result.IsError {
		t.Error("expected error for invalid mode")
	}
}

func TestVoiceCallInitiateNoDefaultTo(t *testing.T) {
	store := NewCallStore(5, nil)
	vt := NewVoiceCallTool(NewMockVoiceCallBackend(), store, VoiceCallToolConfig{
		FromNumber: "+15551234567",
		// No DefaultTo
	}, newTestLogger())

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	if !result.IsError {
		t.Error("expected error for no default_to and no to parameter")
	}
}

func TestVoiceCallInitiateBackendError(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	mock.InitiateResp = nil
	mock.InitiateErr = fmt.Errorf("provider unavailable")
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	if !result.IsError {
		t.Error("expected error for backend failure")
	}
}

func TestVoiceCallInitiateAllowlistBlocked(t *testing.T) {
	store := NewCallStore(5, nil)
	vt := NewVoiceCallTool(NewMockVoiceCallBackend(), store, VoiceCallToolConfig{
		FromNumber:     "+15551234567",
		AllowedNumbers: []string{"+14155551234"},
	}, newTestLogger())

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"to":      "+19995551234",
		"message": "Hello",
	})
	if !result.IsError {
		t.Error("expected error for number not in allowlist")
	}
}

func TestVoiceCallInitiateAllowlistAllowed(t *testing.T) {
	store := NewCallStore(5, nil)
	vt := NewVoiceCallTool(NewMockVoiceCallBackend(), store, VoiceCallToolConfig{
		FromNumber:       "+15551234567",
		AllowedNumbers:   []string{"+14155551234"},
		WebhookPublicURL: "https://example.com",
		WebhookPath:      "/voice/webhook",
		StreamPath:       "/voice/stream",
	}, newTestLogger())

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"to":      "+14155551234",
		"message": "Hello",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestVoiceCallInitiateMaxConcurrent(t *testing.T) {
	store := NewCallStore(1, nil)
	vt := NewVoiceCallTool(NewMockVoiceCallBackend(), store, VoiceCallToolConfig{
		FromNumber:       "+15551234567",
		DefaultTo:        "+15559876543",
		MaxConcurrent:    1,
		WebhookPublicURL: "https://example.com",
		WebhookPath:      "/voice/webhook",
		StreamPath:       "/voice/stream",
	}, newTestLogger())

	// First call should succeed.
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "First",
	})
	if result.IsError {
		t.Fatalf("first call failed: %s", result.Content)
	}

	// Second call should fail (max concurrent = 1).
	result = execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Second",
	})
	if !result.IsError {
		t.Error("expected error for max concurrent")
	}
}

// --- end_call tests ---

func TestVoiceCallEndCallSuccess(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	// Initiate first.
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)

	// End call.
	result = execVoiceCall(t, vt, map[string]any{
		"action":  "end_call",
		"call_id": callID,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if len(mock.HangupCalls) != 1 {
		t.Errorf("expected 1 hangup call, got %d", len(mock.HangupCalls))
	}
}

func TestVoiceCallEndCallMissingID(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action": "end_call",
	})
	if !result.IsError {
		t.Error("expected error for missing call_id")
	}
}

func TestVoiceCallEndCallNotFound(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "end_call",
		"call_id": "nonexistent",
	})
	// Should not be IsError=true (infra error), because end_call doesn't set isInfraError.
	// But it should contain an error message.
	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err == nil {
		if _, hasErr := resp["error"]; !hasErr {
			t.Error("expected error field in response")
		}
	}
}

func TestVoiceCallEndCallAlreadyEnded(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)

	// End first time.
	execVoiceCall(t, vt, map[string]any{"action": "end_call", "call_id": callID})
	// End second time — should note it was already ended.
	result = execVoiceCall(t, vt, map[string]any{"action": "end_call", "call_id": callID})
	if result.IsError {
		t.Fatalf("double end_call should not be an infra error: %s", result.Content)
	}
}

// --- get_status tests ---

func TestVoiceCallGetStatusSuccess(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)

	result = execVoiceCall(t, vt, map[string]any{
		"action":  "get_status",
		"call_id": callID,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var status CallRecord
	if err := json.Unmarshal([]byte(result.Content), &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.State != CallStateInitiated {
		t.Errorf("state = %q, want %q", status.State, CallStateInitiated)
	}
}

func TestVoiceCallGetStatusMissingID(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action": "get_status",
	})
	if !result.IsError {
		t.Error("expected error for missing call_id")
	}
}

// --- speak_to_user tests ---

func TestVoiceCallSpeakToUserSuccess(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)

	result = execVoiceCall(t, vt, map[string]any{
		"action":  "speak_to_user",
		"call_id": callID,
		"message": "How are you?",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mock.PlayTTSCalls) != 1 {
		t.Errorf("expected 1 play_tts call, got %d", len(mock.PlayTTSCalls))
	}
}

func TestVoiceCallSpeakToUserMissingMessage(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)

	result = execVoiceCall(t, vt, map[string]any{
		"action":  "speak_to_user",
		"call_id": callID,
	})
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

func TestVoiceCallSpeakToUserEndedCall(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)
	execVoiceCall(t, vt, map[string]any{"action": "end_call", "call_id": callID})

	result = execVoiceCall(t, vt, map[string]any{
		"action":  "speak_to_user",
		"call_id": callID,
		"message": "Still there?",
	})
	if !result.IsError {
		t.Error("expected error for ended call")
	}
}

// --- continue_call tests ---

func TestVoiceCallContinueCallEndedCall(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	result := execVoiceCall(t, vt, map[string]any{
		"action":  "initiate_call",
		"message": "Hello",
	})
	callID := getCallID(t, result)
	execVoiceCall(t, vt, map[string]any{"action": "end_call", "call_id": callID})

	result = execVoiceCall(t, vt, map[string]any{
		"action":  "continue_call",
		"call_id": callID,
		"message": "Continue?",
	})
	if result.IsError {
		t.Fatalf("ended call continue should not be infra error: %s", result.Content)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ended"] != true {
		t.Error("expected ended=true for ended call")
	}
}

func TestVoiceCallContinueCallMissingID(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "continue_call",
		"message": "Hello",
	})
	if !result.IsError {
		t.Error("expected error for missing call_id")
	}
}

func TestVoiceCallContinueCallMissingMessage(t *testing.T) {
	vt := newTestVoiceCallTool(NewMockVoiceCallBackend())
	result := execVoiceCall(t, vt, map[string]any{
		"action":  "continue_call",
		"call_id": "some-id",
	})
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

// --- CallState tests (table-driven) ---

func TestCallStateTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from     CallState
		to       CallState
		allowed  bool
	}{
		// Forward transitions.
		{"initiated→ringing", CallStateInitiated, CallStateRinging, true},
		{"ringing→answered", CallStateRinging, CallStateAnswered, true},
		{"answered→active", CallStateAnswered, CallStateActive, true},
		{"active→speaking", CallStateActive, CallStateSpeaking, true},
		{"speaking→listening", CallStateSpeaking, CallStateListening, true},
		{"listening→speaking", CallStateListening, CallStateSpeaking, true},

		// Terminal from any non-terminal.
		{"initiated→completed", CallStateInitiated, CallStateCompleted, true},
		{"ringing→no_answer", CallStateRinging, CallStateNoAnswer, true},
		{"active→hangup_user", CallStateActive, CallStateHangupUser, true},
		{"speaking→error", CallStateSpeaking, CallStateError, true},
		{"listening→timeout", CallStateListening, CallStateTimeout, true},
		{"answered→busy", CallStateAnswered, CallStateBusy, true},
		{"initiated→failed", CallStateInitiated, CallStateFailed, true},
		{"ringing→hangup_bot", CallStateRinging, CallStateHangupBot, true},

		// Backward (disallowed).
		{"ringing→initiated", CallStateRinging, CallStateInitiated, false},
		{"active→ringing", CallStateActive, CallStateRinging, false},
		{"listening→active", CallStateListening, CallStateActive, false},

		// Terminal→anything (disallowed).
		{"completed→active", CallStateCompleted, CallStateActive, false},
		{"hangup_user→initiated", CallStateHangupUser, CallStateInitiated, false},
		{"failed→ringing", CallStateFailed, CallStateRinging, false},
		{"error→completed", CallStateError, CallStateCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.from.CanTransitionTo(tt.to)
			if got != tt.allowed {
				t.Errorf("(%s).CanTransitionTo(%s) = %v, want %v",
					tt.from, tt.to, got, tt.allowed)
			}
		})
	}
}

func TestCallStateIsTerminal(t *testing.T) {
	terminal := []CallState{
		CallStateCompleted, CallStateHangupUser, CallStateHangupBot,
		CallStateTimeout, CallStateError, CallStateFailed,
		CallStateNoAnswer, CallStateBusy,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}

	nonTerminal := []CallState{
		CallStateInitiated, CallStateRinging, CallStateAnswered,
		CallStateActive, CallStateSpeaking, CallStateListening,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

// --- CallStore tests ---

func TestCallStoreCreateAndGet(t *testing.T) {
	store := NewCallStore(5, nil)
	record := CallRecord{
		ID:    "test-1",
		To:    "+15551234567",
		From:  "+15559876543",
		Mode:  "notify",
		State: CallStateInitiated,
	}

	if err := store.Create(record); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get("test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "test-1" {
		t.Errorf("ID = %q, want %q", got.ID, "test-1")
	}
	if got.State != CallStateInitiated {
		t.Errorf("State = %q, want %q", got.State, CallStateInitiated)
	}
}

func TestCallStoreGetNotFound(t *testing.T) {
	store := NewCallStore(5, nil)
	_, err := store.Get("nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCallStoreTransition(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	if err := store.Transition("t1", CallStateRinging, ""); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	got, _ := store.Get("t1")
	if got.State != CallStateRinging {
		t.Errorf("State = %q, want %q", got.State, CallStateRinging)
	}
}

func TestCallStoreTransitionInvalid(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})
	store.Transition("t1", CallStateCompleted, "")

	// Terminal → anything is invalid.
	if err := store.Transition("t1", CallStateActive, ""); err == nil {
		t.Error("expected error for terminal→active transition")
	}
}

func TestCallStoreTransitionTerminalSetsEndedAt(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	if err := store.Transition("t1", CallStateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("t1")
	if got.EndedAt == nil {
		t.Error("EndedAt should be set for terminal state")
	}
	if got.Duration <= 0 {
		t.Error("Duration should be > 0 for terminal state")
	}
	if got.ErrorDetail != "done" {
		t.Errorf("ErrorDetail = %q, want %q", got.ErrorDetail, "done")
	}
}

func TestCallStoreMaxConcurrent(t *testing.T) {
	store := NewCallStore(2, nil)
	store.Create(CallRecord{ID: "c1", State: CallStateInitiated})
	store.Create(CallRecord{ID: "c2", State: CallStateInitiated})

	err := store.Create(CallRecord{ID: "c3", State: CallStateInitiated})
	if !errors.Is(err, domain.ErrLimitReached) {
		t.Errorf("expected ErrLimitReached, got %v", err)
	}

	// End one call and try again.
	store.Transition("c1", CallStateCompleted, "")
	if err := store.Create(CallRecord{ID: "c3", State: CallStateInitiated}); err != nil {
		t.Fatalf("Create after freeing slot: %v", err)
	}
}

func TestCallStoreSetProviderCallID(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	if err := store.SetProviderCallID("t1", "provider-123"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("t1")
	if got.ProviderCallID != "provider-123" {
		t.Errorf("ProviderCallID = %q, want %q", got.ProviderCallID, "provider-123")
	}
}

func TestCallStoreFindByProviderID(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})
	store.SetProviderCallID("t1", "provider-123")

	got, err := store.FindByProviderID("provider-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "t1" {
		t.Errorf("ID = %q, want %q", got.ID, "t1")
	}

	_, err = store.FindByProviderID("nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCallStoreActiveCalls(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "a1", State: CallStateInitiated})
	store.Create(CallRecord{ID: "a2", State: CallStateInitiated})
	store.Transition("a2", CallStateCompleted, "")

	active := store.ActiveCalls()
	if len(active) != 1 {
		t.Errorf("expected 1 active call, got %d", len(active))
	}
	if active[0].ID != "a1" {
		t.Errorf("active call ID = %q, want %q", active[0].ID, "a1")
	}
}

func TestCallStoreAppendTranscript(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	if err := store.AppendTranscript("t1", TurnEntry{Role: "bot", Text: "Hello"}); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("t1")
	if len(got.Transcript) != 1 {
		t.Fatalf("expected 1 transcript entry, got %d", len(got.Transcript))
	}
	if got.Transcript[0].Text != "Hello" {
		t.Errorf("text = %q, want %q", got.Transcript[0].Text, "Hello")
	}
}

func TestCallStoreWaitForTranscriptImmediate(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})
	store.AppendTranscript("t1", TurnEntry{Role: "bot", Text: "Hello"})

	// Already has entry at index 0, so waiting from index 0 should return immediately.
	entries, err := store.WaitForTranscript("t1", 0, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestCallStoreWaitForTranscriptTimeout(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	start := time.Now()
	entries, err := store.WaitForTranscript("t1", 0, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on timeout, got %d", len(entries))
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("returned too quickly: %v", elapsed)
	}
}

func TestCallStoreWaitForTranscriptNotify(t *testing.T) {
	store := NewCallStore(5, nil)
	store.Create(CallRecord{ID: "t1", State: CallStateInitiated})

	var entries []TurnEntry
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		entries, _ = store.WaitForTranscript("t1", 0, 5*time.Second)
	}()

	time.Sleep(50 * time.Millisecond)
	store.AppendTranscript("t1", TurnEntry{Role: "user", Text: "Hi"})
	wg.Wait()

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Text != "Hi" {
		t.Errorf("text = %q, want %q", entries[0].Text, "Hi")
	}
}

// --- CallStore concurrent access tests ---

func TestCallStoreConcurrentAccess(t *testing.T) {
	store := NewCallStore(100, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("cc-%d", idx)
			store.Create(CallRecord{ID: id, State: CallStateInitiated})
			store.Transition(id, CallStateRinging, "")
			store.Get(id)
			store.AppendTranscript(id, TurnEntry{Role: "bot", Text: "msg"})
			store.ActiveCalls()
		}(i)
	}
	wg.Wait()
}

// --- FileCallStore tests ---

func TestFileCallStoreAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	record := CallRecord{
		ID:    "fc-1",
		To:    "+15551234567",
		From:  "+15559876543",
		State: CallStateInitiated,
	}

	if err := store.Append(record); err != nil {
		t.Fatal(err)
	}

	record.State = CallStateRinging
	if err := store.Append(record); err != nil {
		t.Fatal(err)
	}

	store.Close()

	// Load from new store.
	store2, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()

	records, err := store2.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (last wins), got %d", len(records))
	}
	if records[0].State != CallStateRinging {
		t.Errorf("state = %q, want %q (last entry wins)", records[0].State, CallStateRinging)
	}
}

func TestFileCallStoreCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calls.jsonl")

	// Write a mix of valid and corrupt lines.
	content := `{"id":"good","state":"initiated","to":"+1555","from":"+1555"}
not-valid-json
{"id":"good","state":"ringing","to":"+1555","from":"+1555"}
`
	os.WriteFile(path, []byte(content), 0600)

	store, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].State != CallStateRinging {
		t.Errorf("state = %q, want %q", records[0].State, CallStateRinging)
	}
}

func TestFileCallStoreLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if records != nil && len(records) != 0 {
		t.Errorf("expected empty records, got %d", len(records))
	}
}

func TestFileCallStoreConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			store.Append(CallRecord{
				ID:    fmt.Sprintf("conc-%d", idx),
				State: CallStateInitiated,
			})
		}(i)
	}
	wg.Wait()

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 20 {
		t.Errorf("expected 20 records, got %d", len(records))
	}
}

// --- CallStore with persistence integration ---

func TestCallStoreWithPersistence(t *testing.T) {
	dir := t.TempDir()
	persistence, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer persistence.Close()

	store := NewCallStore(5, persistence)
	store.Create(CallRecord{ID: "p1", To: "+15551234567", From: "+15559876543", State: CallStateInitiated})
	store.Transition("p1", CallStateRinging, "")
	store.AppendTranscript("p1", TurnEntry{Role: "bot", Text: "Hello"})

	// Reload from persistence.
	persistence2, err := NewFileCallStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer persistence2.Close()

	store2 := NewCallStore(5, persistence2)
	got, err := store2.Get("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != CallStateRinging {
		t.Errorf("state = %q, want %q", got.State, CallStateRinging)
	}
}

// --- Audio codec tests ---

func TestMulawRoundtrip(t *testing.T) {
	// Test that PCM → mu-law → PCM roundtrips within tolerance.
	testSamples := []int16{0, 100, -100, 1000, -1000, 10000, -10000, 32000, -32000}
	for _, s := range testSamples {
		mulaw := LinearToMulaw(s)
		recovered := MulawToLinear(mulaw)
		// Mu-law is lossy, so allow some tolerance.
		diff := int(s) - int(recovered)
		if diff < 0 {
			diff = -diff
		}
		// Tolerance scales with magnitude (mu-law has larger steps at higher amplitudes).
		tolerance := int(float64(absInt16(s)) * 0.15)
		if tolerance < 10 {
			tolerance = 10
		}
		if diff > tolerance {
			t.Errorf("roundtrip(%d): got %d, diff %d > tolerance %d", s, recovered, diff, tolerance)
		}
	}
}

func absInt16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

func TestMulawBufRoundtrip(t *testing.T) {
	pcm := make([]byte, 100)
	for i := 0; i < 50; i++ {
		pcm[i*2] = byte(i * 100)
		pcm[i*2+1] = byte((i * 100) >> 8)
	}

	mulaw := LinearBufToMulaw(pcm)
	if len(mulaw) != 50 {
		t.Fatalf("expected 50 mu-law bytes, got %d", len(mulaw))
	}

	recovered := MulawBufToLinear(mulaw)
	if len(recovered) != 100 {
		t.Fatalf("expected 100 PCM bytes, got %d", len(recovered))
	}
}

func TestResample24kTo8k(t *testing.T) {
	// 6 samples at 24kHz = 2 samples at 8kHz.
	pcm24k := make([]byte, 12)
	for i := 0; i < 6; i++ {
		sample := int16(1000 * (i + 1))
		pcm24k[i*2] = byte(sample)
		pcm24k[i*2+1] = byte(sample >> 8)
	}

	pcm8k := Resample24kTo8k(pcm24k)
	if pcm8k == nil {
		t.Fatal("expected non-nil result")
	}
	if len(pcm8k) != 4 { // 2 samples * 2 bytes
		t.Fatalf("expected 4 bytes, got %d", len(pcm8k))
	}
}

func TestResample24kTo8kEmpty(t *testing.T) {
	result := Resample24kTo8k(nil)
	if result != nil {
		t.Error("expected nil for empty input")
	}
}

func TestResample8kTo24k(t *testing.T) {
	// 2 samples at 8kHz = 6 samples at 24kHz.
	pcm8k := make([]byte, 4)
	pcm8k[0] = 0x00
	pcm8k[1] = 0x10
	pcm8k[2] = 0x00
	pcm8k[3] = 0x20

	pcm24k := Resample8kTo24k(pcm8k)
	if pcm24k == nil {
		t.Fatal("expected non-nil result")
	}
	if len(pcm24k) != 12 { // 6 samples * 2 bytes
		t.Fatalf("expected 12 bytes, got %d", len(pcm24k))
	}
}

func TestResample8kTo24kEmpty(t *testing.T) {
	result := Resample8kTo24k(nil)
	if result != nil {
		t.Error("expected nil for empty input")
	}
}

// --- RingBuffer tests ---

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte{1, 2, 3})
	if rb.Len() != 3 {
		t.Errorf("Len() = %d, want 3", rb.Len())
	}

	data := rb.Read(2)
	if len(data) != 2 || data[0] != 1 || data[1] != 2 {
		t.Errorf("Read(2) = %v, want [1 2]", data)
	}
	if rb.Len() != 1 {
		t.Errorf("Len() = %d, want 1", rb.Len())
	}
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write([]byte{1, 2, 3, 4, 5, 6, 7}) // 7 bytes into 5-byte buffer

	if rb.Len() != 5 {
		t.Errorf("Len() = %d, want 5", rb.Len())
	}

	// Oldest data should be overwritten: should read 3,4,5,6,7
	data := rb.Read(5)
	for i, expected := range []byte{3, 4, 5, 6, 7} {
		if data[i] != expected {
			t.Errorf("data[%d] = %d, want %d", i, data[i], expected)
		}
	}
}

func TestRingBufferClear(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte{1, 2, 3})
	rb.Clear()
	if rb.Len() != 0 {
		t.Errorf("Len() after Clear = %d, want 0", rb.Len())
	}
	data := rb.Read(5)
	if data != nil {
		t.Errorf("Read after Clear = %v, want nil", data)
	}
}

func TestRingBufferReadMoreThanAvailable(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte{1, 2})
	data := rb.Read(10)
	if len(data) != 2 {
		t.Errorf("Read(10) = %d bytes, want 2", len(data))
	}
}

// --- E.164 validation ---

func TestE164Regex(t *testing.T) {
	valid := []string{"+14155551234", "+1234", "+442071234567", "+861012345678"}
	for _, v := range valid {
		if !e164Re.MatchString(v) {
			t.Errorf("expected %q to match E.164", v)
		}
	}

	invalid := []string{"14155551234", "+0123", "not-a-phone", "+", "+1", ""}
	for _, v := range invalid {
		if e164Re.MatchString(v) {
			t.Errorf("expected %q to NOT match E.164", v)
		}
	}
}

// --- TwiML generation ---

func TestGenerateTwiMLNotify(t *testing.T) {
	backend := NewTwilioBackend(TwilioBackendConfig{}, newTestLogger())
	twiml := backend.generateTwiML(InitiateCallRequest{
		Mode:    "notify",
		Message: "Hello world",
	})
	if twiml == "" {
		t.Error("expected non-empty TwiML")
	}
	if !contains(twiml, "<Say>Hello world</Say>") {
		t.Errorf("TwiML should contain Say verb, got: %s", twiml)
	}
	if !contains(twiml, "<Hangup/>") {
		t.Errorf("notify TwiML should contain Hangup, got: %s", twiml)
	}
}

func TestGenerateTwiMLConversation(t *testing.T) {
	backend := NewTwilioBackend(TwilioBackendConfig{}, newTestLogger())
	twiml := backend.generateTwiML(InitiateCallRequest{
		Mode:      "conversation",
		Message:   "Hello",
		StreamURL: "https://example.com/voice/stream?call_id=123",
	})
	if !contains(twiml, "<Connect>") {
		t.Errorf("conversation TwiML should contain Connect, got: %s", twiml)
	}
	if !contains(twiml, "<Stream") {
		t.Errorf("conversation TwiML should contain Stream, got: %s", twiml)
	}
	if !contains(twiml, "wss://") {
		t.Errorf("conversation TwiML should use wss:// for WebSocket, got: %s", twiml)
	}
}

func TestXMLEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{`"quotes" & 'apos'`, "&quot;quotes&quot; &amp; &apos;apos&apos;"},
	}
	for _, tt := range tests {
		got := xmlEscape(tt.input)
		if got != tt.want {
			t.Errorf("xmlEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMapTwilioStatus(t *testing.T) {
	tests := []struct {
		status string
		want   CallState
	}{
		{"queued", CallStateInitiated},
		{"initiated", CallStateInitiated},
		{"ringing", CallStateRinging},
		{"in-progress", CallStateAnswered},
		{"completed", CallStateCompleted},
		{"busy", CallStateBusy},
		{"no-answer", CallStateNoAnswer},
		{"canceled", CallStateHangupBot},
		{"failed", CallStateFailed},
		{"unknown", CallStateError},
	}
	for _, tt := range tests {
		got := mapTwilioStatus(tt.status)
		if got != tt.want {
			t.Errorf("mapTwilioStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// --- HangupActiveCalls ---

func TestHangupActiveCalls(t *testing.T) {
	mock := NewMockVoiceCallBackend()
	vt := newTestVoiceCallTool(mock)

	// Initiate two calls.
	execVoiceCall(t, vt, map[string]any{"action": "initiate_call", "message": "First"})
	execVoiceCall(t, vt, map[string]any{"action": "initiate_call", "message": "Second"})

	// Hangup all active.
	vt.HangupActiveCalls(context.Background())

	if len(mock.HangupCalls) != 2 {
		t.Errorf("expected 2 hangup calls, got %d", len(mock.HangupCalls))
	}

	// All calls should now be terminal.
	active := vt.store.ActiveCalls()
	if len(active) != 0 {
		t.Errorf("expected 0 active calls, got %d", len(active))
	}
}

// --- config defaults ---

func TestVoiceCallToolDefaultConfig(t *testing.T) {
	store := NewCallStore(5, nil)
	vt := NewVoiceCallTool(NewMockVoiceCallBackend(), store, VoiceCallToolConfig{}, newTestLogger())

	if vt.config.Timeout != defaultVoiceCallTimeout {
		t.Errorf("Timeout = %v, want %v", vt.config.Timeout, defaultVoiceCallTimeout)
	}
	if vt.config.MaxDuration != defaultVoiceCallMaxDuration {
		t.Errorf("MaxDuration = %v, want %v", vt.config.MaxDuration, defaultVoiceCallMaxDuration)
	}
	if vt.config.TranscriptTimeout != defaultVoiceCallTranscriptTimeout {
		t.Errorf("TranscriptTimeout = %v, want %v", vt.config.TranscriptTimeout, defaultVoiceCallTranscriptTimeout)
	}
	if vt.config.MaxConcurrent != defaultVoiceCallMaxConcurrent {
		t.Errorf("MaxConcurrent = %d, want %d", vt.config.MaxConcurrent, defaultVoiceCallMaxConcurrent)
	}
	if vt.config.DefaultMode != defaultVoiceCallMode {
		t.Errorf("DefaultMode = %q, want %q", vt.config.DefaultMode, defaultVoiceCallMode)
	}
}

// --- helper ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
