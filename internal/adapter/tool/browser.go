package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
)

// maxJSExpressionLen is the maximum allowed length for JavaScript expressions.
const maxJSExpressionLen = 10240 // 10KB

// maxScreenshotBase64 is the maximum base64 chars returned for a screenshot.
// ~200KB of base64 (~150KB raw JPEG). Larger screenshots are rejected with an
// actionable error message; the backend tries progressive quality reduction first.
const maxScreenshotBase64 = 200000

// blockedJSPatterns are substrings that are not allowed in evaluate expressions.
var blockedJSPatterns = []string{
	"require(",
	"process.exit",
	"child_process",
	"__proto__",
	"constructor.constructor",
}

// blockedJSWordPatterns are patterns that must appear as standalone tokens
// (preceded by start-of-string or a non-alphanumeric character).
// This avoids false positives like "refs.current" or "prefs.theme" matching "fs.".
var blockedJSWordPatterns = []string{
	"fs.",
}

// BrowserTool provides browser automation capabilities.
type BrowserTool struct {
	backend BrowserBackend
	logger  *slog.Logger
}

// NewBrowserTool creates a browser tool backed by the given BrowserBackend.
func NewBrowserTool(backend BrowserBackend, logger *slog.Logger) *BrowserTool {
	return &BrowserTool{backend: backend, logger: logger}
}

func (t *BrowserTool) Name() string { return "browser" }
func (t *BrowserTool) Description() string {
	return "Control a web browser: navigate pages, extract content, click elements, type text, take screenshots, evaluate JavaScript, and manage tabs. Use CSS selectors from get_content output to interact with elements."
}

func (t *BrowserTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["navigate", "get_content", "screenshot", "click", "type", "evaluate", "wait_visible", "tab_list", "tab_open", "tab_close", "tab_focus", "status"],
					"description": "The browser action to perform"
				},
				"url": {
					"type": "string",
					"description": "URL for navigate/tab_open actions"
				},
				"selector": {
					"type": "string",
					"description": "CSS selector for click/type/wait_visible/get_content (scope) actions"
				},
				"text": {
					"type": "string",
					"description": "Text to type for the type action"
				},
				"expression": {
					"type": "string",
					"description": "JavaScript expression for the evaluate action"
				},
				"full_page": {
					"type": "boolean",
					"description": "Capture full scrollable page for screenshot (default: false)"
				},
				"target_id": {
					"type": "string",
					"description": "Tab target ID for tab_close/tab_focus actions"
				}
			},
			"required": ["action"]
		}`),
	}
}

type browserParams struct {
	Action     string `json:"action"`
	URL        string `json:"url,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Text       string `json:"text,omitempty"`
	Expression string `json:"expression,omitempty"`
	FullPage   bool   `json:"full_page,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
}

func (t *BrowserTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.browser", t.logger, params,
		Dispatch(func(p browserParams) string { return p.Action }, ActionMap[browserParams]{
			"navigate":     t.navigate,
			"get_content":  t.getContent,
			"screenshot":   t.screenshot,
			"click":        t.click,
			"type":         t.typeText,
			"evaluate":     t.evaluate,
			"wait_visible": t.waitVisible,
			"tab_list":     t.tabList,
			"tab_open":     t.tabOpen,
			"tab_close":    t.tabClose,
			"tab_focus":    t.tabFocus,
			"status":       t.status,
		}),
	)
}

func (t *BrowserTool) navigate(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("url", strings.TrimSpace(p.URL)); err != nil {
		return nil, err
	}
	if err := security.ValidateURL(p.URL); err != nil {
		return nil, err
	}
	if err := t.backend.Navigate(ctx, p.URL); err != nil {
		return nil, fmt.Errorf("navigate failed: %v", err)
	}
	t.logger.Debug("browser navigated", "url", p.URL)
	return TextResult(fmt.Sprintf("Navigated to %s", p.URL)), nil
}

func (t *BrowserTool) getContent(ctx context.Context, p browserParams) (any, error) {
	content, err := t.backend.GetContent(ctx, p.Selector)
	if err != nil {
		return nil, fmt.Errorf("get content failed: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Page: %s\nURL: %s\n\n", content.Title, content.URL))
	sb.WriteString(content.Text)

	t.logger.Debug("browser content extracted", "title", content.Title, "url", content.URL)
	return TextResult(sb.String()), nil
}

func (t *BrowserTool) screenshot(ctx context.Context, p browserParams) (any, error) {
	data, err := t.backend.Screenshot(ctx, p.FullPage)
	if err != nil {
		return nil, fmt.Errorf("screenshot failed: %v", err)
	}
	t.logger.Debug("browser screenshot captured", "full_page", p.FullPage, "size", len(data))

	if len(data) > maxScreenshotBase64 {
		return nil, fmt.Errorf(
			"screenshot too large (%d chars, max %d) even at lowest quality. "+
				"Try full_page=false or navigate to a simpler page.",
			len(data), maxScreenshotBase64)
	}
	return TextResult(fmt.Sprintf("Screenshot captured (base64, %d chars):\n%s", len(data), data)), nil
}

func (t *BrowserTool) click(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("selector", strings.TrimSpace(p.Selector)); err != nil {
		return nil, err
	}
	if err := t.backend.Click(ctx, p.Selector); err != nil {
		return nil, fmt.Errorf("click failed: %v", err)
	}
	t.logger.Debug("browser clicked", "selector", p.Selector)
	return TextResult(fmt.Sprintf("Clicked element: %s", p.Selector)), nil
}

func (t *BrowserTool) typeText(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("selector", strings.TrimSpace(p.Selector)); err != nil {
		return nil, err
	}
	if err := RequireField("text", p.Text); err != nil {
		return nil, err
	}
	if err := t.backend.Type(ctx, p.Selector, p.Text); err != nil {
		return nil, fmt.Errorf("type failed: %v", err)
	}
	t.logger.Debug("browser typed", "selector", p.Selector, "text_len", len(p.Text))
	return TextResult(fmt.Sprintf("Typed %d chars into element: %s", len(p.Text), p.Selector)), nil
}

func (t *BrowserTool) evaluate(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("expression", strings.TrimSpace(p.Expression)); err != nil {
		return nil, err
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
	result, err := t.backend.Evaluate(ctx, p.Expression)
	if err != nil {
		return nil, fmt.Errorf("evaluate failed: %v", err)
	}
	t.logger.Debug("browser evaluated JS", "expr_len", len(p.Expression))
	return TextResult(result), nil
}

// containsWord checks if pattern appears in s as a standalone token:
// at the start of s, or preceded by a non-alphanumeric character.
// This prevents "refs.current" from matching pattern "fs." while still
// blocking "fs.readFile" and "obj.fs.read".
func containsWord(s, pattern string) bool {
	for i := 0; ; {
		idx := strings.Index(s[i:], pattern)
		if idx < 0 {
			return false
		}
		pos := i + idx
		if pos == 0 || !isAlphanumeric(s[pos-1]) {
			return true
		}
		i = pos + 1
	}
}

func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func (t *BrowserTool) waitVisible(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("selector", strings.TrimSpace(p.Selector)); err != nil {
		return nil, err
	}
	if err := t.backend.WaitVisible(ctx, p.Selector); err != nil {
		return nil, fmt.Errorf("wait_visible failed: %v", err)
	}
	t.logger.Debug("browser wait visible", "selector", p.Selector)
	return TextResult(fmt.Sprintf("Element is visible: %s", p.Selector)), nil
}

func (t *BrowserTool) tabList(ctx context.Context, _ browserParams) (any, error) {
	tabs, err := t.backend.TabList(ctx)
	if err != nil {
		return nil, fmt.Errorf("tab list failed: %v", err)
	}
	data, _ := json.MarshalIndent(tabs, "", "  ")
	t.logger.Debug("browser tab list", "count", len(tabs))
	return TextResult(string(data)), nil
}

func (t *BrowserTool) tabOpen(ctx context.Context, p browserParams) (any, error) {
	if p.URL != "" {
		if err := security.ValidateURL(p.URL); err != nil {
			return nil, err
		}
	}
	targetID, err := t.backend.TabOpen(ctx, p.URL)
	if err != nil {
		return nil, fmt.Errorf("tab open failed: %v", err)
	}
	t.logger.Debug("browser tab opened", "target_id", targetID, "url", p.URL)
	return TextResult(fmt.Sprintf("Opened new tab (target_id: %s)", targetID)), nil
}

func (t *BrowserTool) tabClose(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("target_id", strings.TrimSpace(p.TargetID)); err != nil {
		return nil, err
	}
	if err := t.backend.TabClose(ctx, p.TargetID); err != nil {
		return nil, fmt.Errorf("tab close failed: %v", err)
	}
	t.logger.Debug("browser tab closed", "target_id", p.TargetID)
	return TextResult(fmt.Sprintf("Closed tab: %s", p.TargetID)), nil
}

func (t *BrowserTool) tabFocus(ctx context.Context, p browserParams) (any, error) {
	if err := RequireField("target_id", strings.TrimSpace(p.TargetID)); err != nil {
		return nil, err
	}
	if err := t.backend.TabFocus(ctx, p.TargetID); err != nil {
		return nil, fmt.Errorf("tab focus failed: %v", err)
	}
	t.logger.Debug("browser tab focused", "target_id", p.TargetID)
	return TextResult(fmt.Sprintf("Focused tab: %s", p.TargetID)), nil
}

func (t *BrowserTool) status(ctx context.Context, _ browserParams) (any, error) {
	st, err := t.backend.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("status failed: %v", err)
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	return TextResult(string(data)), nil
}
