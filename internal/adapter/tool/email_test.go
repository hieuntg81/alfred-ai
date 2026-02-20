package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- test backend ---

type testEmailBackend struct {
	emails  []EmailSummary
	message *EmailMessage
	drafts  []*EmailDraft
	sent    []*EmailSendResult
	nextID  int

	listErr   error
	readErr   error
	searchErr error
	draftErr  error
	sendErr   error
	replyErr  error
}

func newTestEmailBackend() *testEmailBackend {
	return &testEmailBackend{nextID: 1}
}

func (b *testEmailBackend) List(_ context.Context, _ ListEmailsOpts) ([]EmailSummary, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	return b.emails, nil
}

func (b *testEmailBackend) Read(_ context.Context, id string) (*EmailMessage, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	if b.message != nil && b.message.ID == id {
		return b.message, nil
	}
	return nil, fmt.Errorf("message %q not found", id)
}

func (b *testEmailBackend) Search(_ context.Context, _ string, _ int) ([]EmailSummary, error) {
	if b.searchErr != nil {
		return nil, b.searchErr
	}
	return b.emails, nil
}

func (b *testEmailBackend) Draft(_ context.Context, to, subject, body string, cc []string) (*EmailDraft, error) {
	if b.draftErr != nil {
		return nil, b.draftErr
	}
	d := &EmailDraft{ID: fmt.Sprintf("draft-%d", b.nextID), To: []string{to}, CC: cc, Subject: subject, Body: body}
	b.nextID++
	b.drafts = append(b.drafts, d)
	return d, nil
}

func (b *testEmailBackend) Send(_ context.Context, to, subject, _ string, _ []string) (*EmailSendResult, error) {
	if b.sendErr != nil {
		return nil, b.sendErr
	}
	r := &EmailSendResult{MessageID: fmt.Sprintf("msg-%d", b.nextID), Status: "sent"}
	b.nextID++
	b.sent = append(b.sent, r)
	return r, nil
}

func (b *testEmailBackend) Reply(_ context.Context, messageID, _ string) (*EmailSendResult, error) {
	if b.replyErr != nil {
		return nil, b.replyErr
	}
	r := &EmailSendResult{MessageID: messageID + "-reply", Status: "sent"}
	b.sent = append(b.sent, r)
	return r, nil
}

// --- helpers ---

func newTestEmailTool(t *testing.T) (*EmailTool, *testEmailBackend) {
	t.Helper()
	b := newTestEmailBackend()
	tool := NewEmailTool(b, 30*time.Second, 100, nil, newTestLogger())
	return tool, b
}

func newTestEmailToolWithDomains(t *testing.T, domains []string) (*EmailTool, *testEmailBackend) {
	t.Helper()
	b := newTestEmailBackend()
	tool := NewEmailTool(b, 30*time.Second, 100, domains, newTestLogger())
	return tool, b
}

func execEmailTool(t *testing.T, tool *EmailTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- metadata ---

func TestEmailToolName(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	if tool.Name() != "email" {
		t.Errorf("got %q, want %q", tool.Name(), "email")
	}
}

func TestEmailToolDescription(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestEmailToolSchema(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	schema := tool.Schema()
	if schema.Name != "email" {
		t.Errorf("schema name: got %q, want %q", schema.Name, "email")
	}
	var params map[string]any
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

// --- action success tests ---

func TestEmailToolList(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.emails = []EmailSummary{{ID: "1", Subject: "hello"}}
	result := execEmailTool(t, tool, map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected email in output: %s", result.Content)
	}
}

func TestEmailToolListEmpty(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "list"})
	if !strings.Contains(result.Content, "No emails found") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestEmailToolRead(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.message = &EmailMessage{ID: "msg-1", Subject: "test", Body: "content"}
	result := execEmailTool(t, tool, map[string]any{"action": "read", "id": "msg-1"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "content") {
		t.Errorf("expected body: %s", result.Content)
	}
}

func TestEmailToolSearch(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.emails = []EmailSummary{{ID: "1", Subject: "match"}}
	result := execEmailTool(t, tool, map[string]any{"action": "search", "query": "match"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestEmailToolSearchEmpty(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "search", "query": "nothing"})
	if !strings.Contains(result.Content, "No emails match") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestEmailToolDraft(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "draft", "to": "user@example.com", "subject": "hi", "body": "content",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "draft") {
		t.Errorf("expected draft ID: %s", result.Content)
	}
}

func TestEmailToolSend(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "user@example.com", "subject": "hi", "body": "content", "confirm": true,
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "sent") {
		t.Errorf("expected sent status: %s", result.Content)
	}
}

func TestEmailToolReply(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "reply", "message_id": "msg-1", "body": "thanks", "confirm": true,
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

// --- safety tests ---

func TestEmailToolSendWithoutConfirm(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "user@example.com", "subject": "hi", "body": "x",
	})
	if !result.IsError {
		t.Error("expected error without confirm")
	}
	if !strings.Contains(result.Content, "confirm") {
		t.Errorf("expected confirm message: %s", result.Content)
	}
}

func TestEmailToolReplyWithoutConfirm(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "reply", "message_id": "msg-1", "body": "x",
	})
	if !result.IsError {
		t.Error("expected error without confirm")
	}
}

func TestEmailToolSendRateLimit(t *testing.T) {
	b := newTestEmailBackend()
	tool := NewEmailTool(b, 30*time.Second, 2, nil, newTestLogger())

	for i := 0; i < 2; i++ {
		result := execEmailTool(t, tool, map[string]any{
			"action": "send", "to": "u@x.com", "subject": "s", "body": "b", "confirm": true,
		})
		if result.IsError {
			t.Fatalf("send %d failed: %s", i+1, result.Content)
		}
	}

	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "u@x.com", "subject": "s", "body": "b", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected rate limit error")
	}
	if !strings.Contains(result.Content, "rate limit") {
		t.Errorf("expected rate limit message: %s", result.Content)
	}
}

func TestEmailToolReplyRateLimit(t *testing.T) {
	b := newTestEmailBackend()
	tool := NewEmailTool(b, 30*time.Second, 1, nil, newTestLogger())

	execEmailTool(t, tool, map[string]any{
		"action": "reply", "message_id": "m1", "body": "b", "confirm": true,
	})

	result := execEmailTool(t, tool, map[string]any{
		"action": "reply", "message_id": "m2", "body": "b", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected rate limit error for reply")
	}
}

// --- domain allowlist ---

func TestEmailToolDomainAllowlistBlocks(t *testing.T) {
	tool, _ := newTestEmailToolWithDomains(t, []string{"allowed.com"})
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "user@blocked.com", "subject": "s", "body": "b", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected error for blocked domain")
	}
	if !strings.Contains(result.Content, "not in the allowed list") {
		t.Errorf("expected domain error: %s", result.Content)
	}
}

func TestEmailToolDomainAllowlistAllows(t *testing.T) {
	tool, _ := newTestEmailToolWithDomains(t, []string{"allowed.com"})
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "user@allowed.com", "subject": "s", "body": "b", "confirm": true,
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestEmailToolDomainAllowlistDraft(t *testing.T) {
	tool, _ := newTestEmailToolWithDomains(t, []string{"ok.com"})
	result := execEmailTool(t, tool, map[string]any{
		"action": "draft", "to": "user@bad.com", "subject": "s", "body": "b",
	})
	if !result.IsError {
		t.Error("expected domain error for draft too")
	}
}

func TestEmailToolDomainAllowlistCaseInsensitive(t *testing.T) {
	tool, _ := newTestEmailToolWithDomains(t, []string{"Example.COM"})
	result := execEmailTool(t, tool, map[string]any{
		"action": "draft", "to": "user@example.com", "subject": "s", "body": "b",
	})
	if result.IsError {
		t.Fatalf("expected success (case insensitive): %s", result.Content)
	}
}

func TestEmailToolInvalidEmailAddress(t *testing.T) {
	tool, _ := newTestEmailToolWithDomains(t, []string{"x.com"})
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "nope", "subject": "s", "body": "b", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected error for invalid email")
	}
}

// --- validation error tests ---

func TestEmailToolReadMissingID(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "read"})
	if !result.IsError {
		t.Error("expected error for missing id")
	}
}

func TestEmailToolSearchMissingQuery(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "search"})
	if !result.IsError {
		t.Error("expected error for missing query")
	}
}

func TestEmailToolDraftMissingFields(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "draft"})
	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestEmailToolSendMissingFields(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected error for missing fields")
	}
}

func TestEmailToolReplyMissingFields(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{
		"action": "reply", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected error for missing message_id/body")
	}
}

func TestEmailToolUnknownAction(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result := execEmailTool(t, tool, map[string]any{"action": "bad"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestEmailToolInvalidJSON(t *testing.T) {
	tool, _ := newTestEmailTool(t)
	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

// --- backend error propagation ---

func TestEmailToolBackendListError(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.listErr = fmt.Errorf("backend error")
	result := execEmailTool(t, tool, map[string]any{"action": "list"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestEmailToolBackendReadError(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.readErr = fmt.Errorf("backend error")
	result := execEmailTool(t, tool, map[string]any{"action": "read", "id": "1"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestEmailToolBackendSendError(t *testing.T) {
	tool, backend := newTestEmailTool(t)
	backend.sendErr = fmt.Errorf("smtp error")
	result := execEmailTool(t, tool, map[string]any{
		"action": "send", "to": "u@x.com", "subject": "s", "body": "b", "confirm": true,
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestEmailToolNilBackendUsesMock(t *testing.T) {
	tool := NewEmailTool(nil, 30*time.Second, 100, nil, newTestLogger())
	result := execEmailTool(t, tool, map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("expected success with mock: %s", result.Content)
	}
}

// --- fuzz ---

func FuzzEmailTool_Execute(f *testing.F) {
	f.Add([]byte(`{"action":"list"}`))
	f.Add([]byte(`{"action":"send","to":"u@x.com","subject":"s","body":"b","confirm":true}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))

	b := newTestEmailBackend()
	tool := NewEmailTool(b, 30*time.Second, 10000, nil, newTestLogger())
	f.Fuzz(func(t *testing.T, data []byte) {
		tool.Execute(context.Background(), data)
	})
}
