package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// maxResponseBody is the maximum response body size we read from LLM APIs.
const maxResponseBody = 10 * 1024 * 1024 // 10 MB

// doJSONRequest performs a JSON POST request and returns the response body.
// It handles: create request, set headers, execute, read body (with limit),
// and check HTTP status code. Returns a domain error for non-200 responses.
func doJSONRequest(ctx context.Context, client *http.Client, url string, body []byte, headers map[string]string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, mapHTTPError(httpResp.StatusCode, respBody)
	}

	return respBody, nil
}

// doStreamRequest performs a JSON POST request for SSE streaming.
// It returns the open *http.Response (caller must close Body).
// Returns a domain error for non-200 responses.
func doStreamRequest(ctx context.Context, client *http.Client, url string, body []byte, headers map[string]string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return nil, mapHTTPError(httpResp.StatusCode, respBody)
	}

	return httpResp, nil
}

// logChatCompleted logs the standard debug message after a successful LLM chat.
func logChatCompleted(logger *slog.Logger, providerName string, result *domain.ChatResponse) {
	logger.Debug("llm chat completed",
		"provider", providerName,
		"model", result.Model,
		"tokens", result.Usage.TotalTokens,
	)
}

// setUsageAttrs adds token usage attributes to a trace span.
func setUsageAttrs(span trace.Span, usage domain.Usage) {
	span.SetAttributes(
		tracer.IntAttr("llm.prompt_tokens", usage.PromptTokens),
		tracer.IntAttr("llm.completion_tokens", usage.CompletionTokens),
	)
}

// mapHTTPError maps an HTTP status code + response body to a domain error.
// This enables ErrorClassifier, circuit breaker, and auto-compression to
// correctly classify LLM API errors.
func mapHTTPError(statusCode int, body []byte) error {
	bodyStr := string(body)
	detail := fmt.Sprintf("API error %d: %s", statusCode, bodyStr)

	switch {
	case statusCode == http.StatusTooManyRequests: // 429
		return fmt.Errorf("%w: %s", domain.ErrRateLimit, detail)
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden: // 401, 403
		return fmt.Errorf("%w: %s", domain.ErrAuthInvalid, detail)
	case statusCode == http.StatusRequestEntityTooLarge: // 413
		return fmt.Errorf("%w: %s", domain.ErrContextOverflow, detail)
	case statusCode >= 500: // 500, 502, 503, etc.
		// Server errors are retryable.
		return fmt.Errorf("%w: %s", domain.ErrToolFailure, detail)
	default:
		return fmt.Errorf("%s", detail)
	}
}
