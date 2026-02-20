package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// Canvas content limits.
const (
	defaultMaxCanvasContentSize = 512 * 1024 // 512KB
	maxCanvasNameLen            = 128
	maxCanvasesPerSession       = 50
)

// canvasNameRegex validates canvas names: alphanumeric, hyphens, underscores.
var canvasNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// blockedHTMLPatterns are substrings blocked in canvas content for security.
var blockedHTMLPatterns = []string{
	"<script src=",
	"fetch(",
	"XMLHttpRequest",
	"navigator.sendBeacon",
	"window.open(",
	"document.cookie",
	"localStorage",
	"sessionStorage",
	"indexedDB",
	"importScripts",
	"eval(",
	"Function(",
}

// CanvasTool provides canvas creation and management for interactive HTML content.
type CanvasTool struct {
	backend CanvasBackend
	bus     domain.EventBus
	logger  *slog.Logger
	maxSize int
}

// NewCanvasTool creates a canvas tool backed by the given CanvasBackend.
// Session ID is read from context at execution time.
func NewCanvasTool(
	backend CanvasBackend,
	bus domain.EventBus,
	maxSize int,
	logger *slog.Logger,
) *CanvasTool {
	if maxSize <= 0 {
		maxSize = defaultMaxCanvasContentSize
	}
	return &CanvasTool{
		backend: backend,
		bus:     bus,
		logger:  logger,
		maxSize: maxSize,
	}
}

// sessionID extracts the session ID from context, returning an error if missing.
func (t *CanvasTool) sessionID(ctx context.Context) (string, error) {
	id := domain.SessionIDFromContext(ctx)
	if id == "" {
		return "", fmt.Errorf("no session context available")
	}
	return id, nil
}

func (t *CanvasTool) Name() string { return "canvas" }
func (t *CanvasTool) Description() string {
	return "Create and manage interactive HTML/CSS/JS canvases. " +
		"Use to build visual interfaces, charts, dashboards, " +
		"interactive demos, or any rich content for the user."
}

func (t *CanvasTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["create", "update", "read", "delete", "list", "present", "hide", "snapshot", "eval_js"],
					"description": "The canvas action to perform"
				},
				"name": {
					"type": "string",
					"description": "Canvas name/identifier (alphanumeric, hyphens, underscores)"
				},
				"content": {
					"type": "string",
					"description": "HTML/CSS/JS content for create/update actions"
				},
				"expression": {
					"type": "string",
					"description": "JavaScript expression for eval_js action"
				}
			},
			"required": ["action"]
		}`),
	}
}

type canvasParams struct {
	Action     string `json:"action"`
	Name       string `json:"name,omitempty"`
	Content    string `json:"content,omitempty"`
	Expression string `json:"expression,omitempty"`
}

func (t *CanvasTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.canvas", t.logger, params,
		Dispatch(func(p canvasParams) string { return p.Action }, ActionMap[canvasParams]{
			"create":   t.create,
			"update":   t.update,
			"read":     t.read,
			"delete":   t.doDelete,
			"list":     t.list,
			"present":  t.present,
			"hide":     t.hide,
			"snapshot": t.snapshot,
			"eval_js":  t.evalJS,
		}),
	)
}

func (t *CanvasTool) create(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Content) == "" {
		return nil, fmt.Errorf("content is required for create action")
	}
	if len(p.Content) > t.maxSize {
		return nil, fmt.Errorf("content too large: %d bytes (max %d)", len(p.Content), t.maxSize)
	}
	if err := validateCanvasContent(p.Content); err != nil {
		return nil, err
	}

	existing, err := t.backend.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list canvases failed: %v", err)
	}
	if len(existing) >= maxCanvasesPerSession {
		return nil, fmt.Errorf("canvas limit reached (%d per session)", maxCanvasesPerSession)
	}

	info, err := t.backend.Create(ctx, sessionID, p.Name, p.Content)
	if err != nil {
		return nil, fmt.Errorf("create failed: %v", err)
	}

	PublishToolEvent(ctx, t.bus, domain.EventCanvasCreated, info)
	t.logger.Debug("canvas created", "name", p.Name, "size", info.Size)
	return TextResult(fmt.Sprintf("Canvas %q created (%d bytes). Use action=present to display it.", p.Name, info.Size)), nil
}

func (t *CanvasTool) update(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Content) == "" {
		return nil, fmt.Errorf("content is required for update action")
	}
	if len(p.Content) > t.maxSize {
		return nil, fmt.Errorf("content too large: %d bytes (max %d)", len(p.Content), t.maxSize)
	}
	if err := validateCanvasContent(p.Content); err != nil {
		return nil, err
	}

	info, err := t.backend.Update(ctx, sessionID, p.Name, p.Content)
	if err != nil {
		return nil, fmt.Errorf("update failed: %v", err)
	}

	PublishToolEvent(ctx, t.bus, domain.EventCanvasUpdated, info)
	t.logger.Debug("canvas updated", "name", p.Name, "size", info.Size)
	return TextResult(fmt.Sprintf("Canvas %q updated (%d bytes)", p.Name, info.Size)), nil
}

func (t *CanvasTool) read(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}

	content, err := t.backend.Read(ctx, sessionID, p.Name)
	if err != nil {
		return nil, fmt.Errorf("read failed: %v", err)
	}

	t.logger.Debug("canvas read", "name", p.Name, "size", content.Size)
	return TextResult(content.Content), nil
}

func (t *CanvasTool) doDelete(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}

	if err := t.backend.Delete(ctx, sessionID, p.Name); err != nil {
		return nil, fmt.Errorf("delete failed: %v", err)
	}

	PublishToolEvent(ctx, t.bus, domain.EventCanvasDeleted, &CanvasInfo{
		Name: p.Name, SessionID: sessionID,
	})
	t.logger.Debug("canvas deleted", "name", p.Name)
	return TextResult(fmt.Sprintf("Canvas %q deleted", p.Name)), nil
}

func (t *CanvasTool) list(ctx context.Context, _ canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	canvases, err := t.backend.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list failed: %v", err)
	}

	if len(canvases) == 0 {
		return TextResult("No canvases in current session."), nil
	}

	data, _ := json.MarshalIndent(canvases, "", "  ")
	t.logger.Debug("canvas list", "count", len(canvases))
	return TextResult(string(data)), nil
}

func (t *CanvasTool) present(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}

	content, err := t.backend.Read(ctx, sessionID, p.Name)
	if err != nil {
		return nil, fmt.Errorf("canvas not found: %v", err)
	}

	PublishToolEvent(ctx, t.bus, domain.EventCanvasPresented, content)
	t.logger.Debug("canvas presented", "name", p.Name)
	return TextResult(fmt.Sprintf("Canvas %q is now displayed to the user (%d bytes)", p.Name, content.Size)), nil
}

func (t *CanvasTool) hide(ctx context.Context, _ canvasParams) (any, error) {
	PublishToolEvent(ctx, t.bus, domain.EventCanvasHidden, nil)
	t.logger.Debug("canvas hidden")
	return TextResult("Canvas panel hidden"), nil
}

func (t *CanvasTool) snapshot(ctx context.Context, p canvasParams) (any, error) {
	sessionID, err := t.sessionID(ctx)
	if err != nil {
		return nil, err
	}
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}

	content, err := t.backend.Read(ctx, sessionID, p.Name)
	if err != nil {
		return nil, fmt.Errorf("snapshot failed: %v", err)
	}

	t.logger.Debug("canvas snapshot", "name", p.Name, "size", content.Size)
	return TextResult(fmt.Sprintf("Snapshot of canvas %q (%d bytes, updated %s):\n\n%s",
		p.Name, content.Size, content.UpdatedAt.Format(time.RFC3339), content.Content)), nil
}

func (t *CanvasTool) evalJS(ctx context.Context, p canvasParams) (any, error) {
	if strings.TrimSpace(p.Expression) == "" {
		return nil, fmt.Errorf("expression is required for eval_js action")
	}
	if len(p.Expression) > maxJSExpressionLen {
		return nil, fmt.Errorf("expression too long: %d chars (max %d)", len(p.Expression), maxJSExpressionLen)
	}

	lower := strings.ToLower(p.Expression)
	for _, pattern := range blockedJSPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return nil, fmt.Errorf("expression blocked: contains prohibited pattern %q", pattern)
		}
	}
	for _, pattern := range blockedJSWordPatterns {
		if containsWord(lower, strings.ToLower(pattern)) {
			return nil, fmt.Errorf("expression blocked: contains prohibited pattern %q", pattern)
		}
	}

	PublishToolEvent(ctx, t.bus, domain.EventCanvasEvalJS, &CanvasEvalRequest{
		CanvasName: p.Name,
		Expression: p.Expression,
	})

	t.logger.Debug("canvas eval_js requested", "expr_len", len(p.Expression))
	return TextResult(fmt.Sprintf("JavaScript evaluation requested (%d chars). "+
		"Result will be available when a renderer processes the event.", len(p.Expression))), nil
}

func (t *CanvasTool) validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxCanvasNameLen {
		return fmt.Errorf("name too long: %d chars (max %d)", len(name), maxCanvasNameLen)
	}
	if !canvasNameRegex.MatchString(name) {
		return fmt.Errorf("invalid canvas name %q: must be alphanumeric with hyphens/underscores, starting with alphanumeric", name)
	}
	return nil
}

func validateCanvasContent(content string) error {
	lower := strings.ToLower(content)
	for _, pattern := range blockedHTMLPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return fmt.Errorf("content blocked: contains prohibited pattern %q", pattern)
		}
	}
	return nil
}

