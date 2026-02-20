package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
)

const defaultMaxBodySize = 1 * 1024 * 1024 // 1MB

// WebTool fetches content from URLs with SSRF protection.
type WebTool struct {
	client      *http.Client
	maxBodySize int64
	logger      *slog.Logger
}

// NewWebTool creates a web fetch tool with SSRF protection.
func NewWebTool(logger *slog.Logger) *WebTool {
	return &WebTool{
		client: &http.Client{
			Transport: security.NewSSRFSafeTransport(), // Use safe transport to prevent DNS rebinding
			Timeout:   30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				// Validate each redirect target for SSRF
				if err := security.ValidateURL(req.URL.String()); err != nil {
					return err
				}
				return nil
			},
		},
		maxBodySize: defaultMaxBodySize,
		logger:      logger,
	}
}

func (t *WebTool) Name() string        { return "web_fetch" }
func (t *WebTool) Description() string { return "Fetch content from a URL (SSRF protected)" }

func (t *WebTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "The URL to fetch"},
				"method": {"type": "string", "enum": ["GET", "HEAD"], "description": "HTTP method (default: GET)"},
				"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Additional HTTP headers"}
			},
			"required": ["url"]
		}`),
	}
}

type webParams struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (t *WebTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.web_fetch", t.logger, params,
		func(ctx context.Context, span trace.Span, p webParams) (any, error) {
			// SSRF check
			if err := security.ValidateURL(p.URL); err != nil {
				return nil, err
			}

			method := p.Method
			if method == "" {
				method = http.MethodGet
			}

			// Security: Only allow safe HTTP methods
			if method != http.MethodGet && method != http.MethodHead {
				return nil, fmt.Errorf("invalid HTTP method: %q (only GET and HEAD allowed)", method)
			}

			req, err := http.NewRequestWithContext(ctx, method, p.URL, nil)
			if err != nil {
				return nil, fmt.Errorf("create request: %v", err)
			}

			// Security: Validate headers for CRLF injection
			for k, v := range p.Headers {
				if containsCRLF(k) || containsCRLF(v) {
					return nil, fmt.Errorf("invalid header: CRLF characters not allowed")
				}
				req.Header.Set(k, v)
			}

			resp, err := t.client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("http request: %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxBodySize))
			if err != nil {
				return nil, fmt.Errorf("read body: %v", err)
			}

			result := fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, string(body))

			t.logger.Debug("web fetch completed", "url", p.URL, "status", resp.StatusCode, "size", len(body))
			return result, nil
		},
	)
}

// containsCRLF checks if a string contains CRLF characters that could be used for header injection.
func containsCRLF(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}
