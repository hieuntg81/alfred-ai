package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// Email data types.

// ListEmailsOpts controls email listing.
type ListEmailsOpts struct {
	Folder  string `json:"folder,omitempty"` // "inbox", "sent", etc.
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// EmailSummary describes an email without full body.
type EmailSummary struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Date    string   `json:"date"`
	Snippet string   `json:"snippet"`
}

// EmailMessage is a full email with body.
type EmailMessage struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	Date    string   `json:"date"`
}

// EmailDraft represents a draft email.
type EmailDraft struct {
	ID      string   `json:"id"`
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

// EmailSendResult is the result of sending an email.
type EmailSendResult struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

// EmailBackend abstracts email operations.
type EmailBackend interface {
	List(ctx context.Context, opts ListEmailsOpts) ([]EmailSummary, error)
	Read(ctx context.Context, id string) (*EmailMessage, error)
	Search(ctx context.Context, query string, limit int) ([]EmailSummary, error)
	Draft(ctx context.Context, to, subject, body string, cc []string) (*EmailDraft, error)
	Send(ctx context.Context, to, subject, body string, cc []string) (*EmailSendResult, error)
	Reply(ctx context.Context, messageID, body string) (*EmailSendResult, error)
}

// MockEmailBackend is a no-op backend for testing/development.
type MockEmailBackend struct {
	nextID int
}

func NewMockEmailBackend() *MockEmailBackend { return &MockEmailBackend{nextID: 1} }

func (m *MockEmailBackend) List(_ context.Context, _ ListEmailsOpts) ([]EmailSummary, error) {
	return nil, nil
}
func (m *MockEmailBackend) Read(_ context.Context, id string) (*EmailMessage, error) {
	return nil, fmt.Errorf("message %q not found", id)
}
func (m *MockEmailBackend) Search(_ context.Context, _ string, _ int) ([]EmailSummary, error) {
	return nil, nil
}
func (m *MockEmailBackend) Draft(_ context.Context, to, subject, body string, cc []string) (*EmailDraft, error) {
	id := fmt.Sprintf("draft-%d", m.nextID)
	m.nextID++
	return &EmailDraft{ID: id, To: []string{to}, CC: cc, Subject: subject, Body: body}, nil
}
func (m *MockEmailBackend) Send(_ context.Context, to, subject, _ string, _ []string) (*EmailSendResult, error) {
	id := fmt.Sprintf("msg-%d", m.nextID)
	m.nextID++
	return &EmailSendResult{MessageID: id, Status: "sent"}, nil
}
func (m *MockEmailBackend) Reply(_ context.Context, messageID, _ string) (*EmailSendResult, error) {
	return &EmailSendResult{MessageID: messageID + "-reply", Status: "sent"}, nil
}

// EmailTool provides email operations to the LLM.
type EmailTool struct {
	backend        EmailBackend
	logger         *slog.Logger
	sendLimiter    *RateLimiter
	allowedDomains []string
}

// NewEmailTool creates an email tool. If backend is nil, a MockEmailBackend is used.
func NewEmailTool(
	backend EmailBackend,
	timeout time.Duration,
	maxSendsPerHour int,
	allowedDomains []string,
	logger *slog.Logger,
) *EmailTool {
	if backend == nil {
		backend = NewMockEmailBackend()
	}
	return &EmailTool{
		backend:        backend,
		logger:         logger,
		sendLimiter:    NewRateLimiter(maxSendsPerHour, time.Hour),
		allowedDomains: allowedDomains,
	}
}

func (t *EmailTool) Name() string { return "email" }
func (t *EmailTool) Description() string {
	return "Manage email: list inbox, read messages, search, draft, send, and reply. " +
		"Send and reply require explicit confirmation (confirm: true)."
}

func (t *EmailTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list", "read", "search", "draft", "send", "reply"],
					"description": "The email action to perform"
				},
				"id": {
					"type": "string",
					"description": "Message ID (for read action)"
				},
				"to": {
					"type": "string",
					"description": "Recipient email address"
				},
				"cc": {
					"type": "array",
					"items": {"type": "string"},
					"description": "CC recipients"
				},
				"subject": {
					"type": "string",
					"description": "Email subject"
				},
				"body": {
					"type": "string",
					"description": "Email body text"
				},
				"query": {
					"type": "string",
					"description": "Search query"
				},
				"message_id": {
					"type": "string",
					"description": "Message ID to reply to"
				},
				"confirm": {
					"type": "boolean",
					"description": "Must be true to send/reply (safety gate)"
				},
				"folder": {
					"type": "string",
					"description": "Folder to list (e.g. inbox, sent)"
				},
				"limit": {
					"type": "integer",
					"description": "Max results for search"
				},
				"page": {
					"type": "integer",
					"description": "Page number for list"
				},
				"per_page": {
					"type": "integer",
					"description": "Results per page"
				}
			},
			"required": ["action"]
		}`),
	}
}

type emailParams struct {
	Action    string   `json:"action"`
	ID        string   `json:"id,omitempty"`
	To        string   `json:"to,omitempty"`
	CC        []string `json:"cc,omitempty"`
	Subject   string   `json:"subject,omitempty"`
	Body      string   `json:"body,omitempty"`
	Query     string   `json:"query,omitempty"`
	MessageID string   `json:"message_id,omitempty"`
	Confirm   bool     `json:"confirm,omitempty"`
	Folder    string   `json:"folder,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Page      int      `json:"page,omitempty"`
	PerPage   int      `json:"per_page,omitempty"`
}

func (t *EmailTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.email", t.logger, params,
		Dispatch(func(p emailParams) string { return p.Action }, ActionMap[emailParams]{
			"list":   t.handleList,
			"read":   t.handleRead,
			"search": t.handleSearch,
			"draft":  t.handleDraft,
			"send":   t.handleSend,
			"reply":  t.handleReply,
		}),
	)
}

func (t *EmailTool) checkDomain(email string) error {
	if len(t.allowedDomains) == 0 {
		return nil
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid email address %q", email)
	}
	domain := strings.ToLower(parts[1])
	for _, d := range t.allowedDomains {
		if strings.ToLower(d) == domain {
			return nil
		}
	}
	return fmt.Errorf("domain %q is not in the allowed list", domain)
}

func (t *EmailTool) handleList(ctx context.Context, p emailParams) (any, error) {
	emails, err := t.backend.List(ctx, ListEmailsOpts{
		Folder: p.Folder, Page: p.Page, PerPage: p.PerPage,
	})
	if err != nil {
		return nil, err
	}
	if len(emails) == 0 {
		return TextResult("No emails found."), nil
	}
	return emails, nil
}

func (t *EmailTool) handleRead(ctx context.Context, p emailParams) (any, error) {
	if err := RequireField("id", p.ID); err != nil {
		return nil, err
	}
	return t.backend.Read(ctx, p.ID)
}

func (t *EmailTool) handleSearch(ctx context.Context, p emailParams) (any, error) {
	if err := RequireField("query", p.Query); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	results, err := t.backend.Search(ctx, p.Query, limit)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return TextResult("No emails match the search query."), nil
	}
	return results, nil
}

func (t *EmailTool) handleDraft(ctx context.Context, p emailParams) (any, error) {
	if err := RequireFields("to", p.To, "subject", p.Subject, "body", p.Body); err != nil {
		return nil, err
	}
	if err := t.checkDomain(p.To); err != nil {
		return nil, err
	}
	return t.backend.Draft(ctx, p.To, p.Subject, p.Body, p.CC)
}

func (t *EmailTool) handleSend(ctx context.Context, p emailParams) (any, error) {
	if !p.Confirm {
		return nil, fmt.Errorf("'confirm' must be true to send email (safety requirement)")
	}
	if err := RequireFields("to", p.To, "subject", p.Subject, "body", p.Body); err != nil {
		return nil, err
	}
	if err := t.checkDomain(p.To); err != nil {
		return nil, err
	}
	if !t.sendLimiter.Allow() {
		return nil, fmt.Errorf("send rate limit exceeded (max sends per hour reached)")
	}
	t.logger.Info("sending email", "to", p.To, "subject", p.Subject)
	return t.backend.Send(ctx, p.To, p.Subject, p.Body, p.CC)
}

func (t *EmailTool) handleReply(ctx context.Context, p emailParams) (any, error) {
	if !p.Confirm {
		return nil, fmt.Errorf("'confirm' must be true to send reply (safety requirement)")
	}
	if err := RequireFields("message_id", p.MessageID, "body", p.Body); err != nil {
		return nil, err
	}
	if !t.sendLimiter.Allow() {
		return nil, fmt.Errorf("send rate limit exceeded (max sends per hour reached)")
	}
	t.logger.Info("replying to email", "message_id", p.MessageID)
	return t.backend.Reply(ctx, p.MessageID, p.Body)
}
