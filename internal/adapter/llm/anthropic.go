package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/infra/tracer"
)

const defaultAnthropicVersion = "2023-06-01"

// AnthropicProvider implements domain.LLMProvider for the Anthropic Messages API.
type AnthropicProvider struct {
	name    string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
	logger  *slog.Logger
	version string
}

// NewAnthropicProvider creates a provider for the Anthropic Messages API.
func NewAnthropicProvider(cfg config.ProviderConfig, logger *slog.Logger) *AnthropicProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	return &AnthropicProvider{
		name:    cfg.Name,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  NewHTTPClient(cfg),
		logger:  logger,
		version: defaultAnthropicVersion,
	}
}

// Chat implements domain.LLMProvider.
func (p *AnthropicProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	ctx, span := tracer.StartSpan(ctx, "llm.chat",
		trace.WithAttributes(
			tracer.StringAttr("llm.provider", p.name),
			tracer.StringAttr("llm.model", req.Model),
		),
	)
	defer span.End()

	if req.Model == "" {
		req.Model = p.model
	}

	body, err := json.Marshal(toAnthropicRequest(req))
	if err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": p.version,
	}

	respBody, err := doJSONRequest(ctx, p.client, p.baseURL+"/v1/messages", body, headers)
	if err != nil {
		tracer.RecordError(span, err)
		return nil, err
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := fromAnthropicResponse(antResp)
	setUsageAttrs(span, result.Usage)
	tracer.SetOK(span)
	logChatCompleted(p.logger, p.name, result)

	return result, nil
}

// Name implements domain.LLMProvider.
func (p *AnthropicProvider) Name() string { return p.name }

// --- Anthropic API wire types ---

type anthropicRequest struct {
	Model     string              `json:"model"`
	Messages  []anthropicMessage  `json:"messages"`
	System    string              `json:"system,omitempty"`
	MaxTokens int                 `json:"max_tokens"`
	Tools     []anthropicTool     `json:"tools,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
	Thinking  *anthropicThinking  `json:"thinking,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
	Usage   anthropicUsage     `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Anthropic streaming wire types ---

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
	Usage json.RawMessage `json:"usage,omitempty"`

	// content_block_start fields
	ContentBlock *anthropicContent `json:"content_block,omitempty"`
}

type anthropicDeltaText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicDeltaToolInput struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type anthropicDeltaThinking struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

// ChatStream implements domain.StreamingLLMProvider.
func (p *AnthropicProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	if req.Model == "" {
		req.Model = p.model
	}

	antReq := toAnthropicRequest(req)
	antReq.Stream = true

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": p.version,
	}

	httpResp, err := doStreamRequest(ctx, p.client, p.baseURL+"/v1/messages", body, headers)
	if err != nil {
		return nil, err
	}

	// Anthropic uses "event: <type>\ndata: <json>" pairs, but our SSE parser
	// only looks at "data:" lines. We need to also capture the preceding
	// "event:" line to know the event type. We handle this by embedding the
	// event type dispatch inside the data parser since the data JSON contains
	// a "type" field that maps to the SSE event type.
	ch := parseSSEStream(ctx, httpResp.Body, func(data []byte) (*domain.StreamDelta, error) {
		var evt anthropicStreamEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return nil, err
		}

		switch evt.Type {
		case "content_block_delta":
			// Try text delta first
			var td anthropicDeltaText
			if err := json.Unmarshal(evt.Delta, &td); err == nil && td.Type == "text_delta" {
				return &domain.StreamDelta{Content: td.Text}, nil
			}
			// Try thinking delta
			var tk anthropicDeltaThinking
			if err := json.Unmarshal(evt.Delta, &tk); err == nil && tk.Type == "thinking_delta" {
				return &domain.StreamDelta{Thinking: tk.Thinking}, nil
			}
			// Try tool input delta
			var ti anthropicDeltaToolInput
			if err := json.Unmarshal(evt.Delta, &ti); err == nil && ti.Type == "input_json_delta" {
				return &domain.StreamDelta{Content: ti.PartialJSON}, nil
			}
			return nil, nil

		case "content_block_start":
			if evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
				return &domain.StreamDelta{
					ToolCalls: []domain.ToolCall{{
						ID:   evt.ContentBlock.ID,
						Name: evt.ContentBlock.Name,
					}},
				}, nil
			}
			return nil, nil

		case "message_delta":
			delta := &domain.StreamDelta{Done: true}
			if len(evt.Usage) > 0 {
				var u anthropicUsage
				if err := json.Unmarshal(evt.Usage, &u); err == nil {
					delta.Usage = &domain.Usage{
						PromptTokens:     u.InputTokens,
						CompletionTokens: u.OutputTokens,
						TotalTokens:      u.InputTokens + u.OutputTokens,
					}
				}
			}
			return delta, nil

		case "message_stop":
			return &domain.StreamDelta{Done: true}, nil

		default:
			return nil, nil
		}
	})

	return ch, nil
}

func toAnthropicRequest(req domain.ChatRequest) anthropicRequest {
	antReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}

	if antReq.MaxTokens <= 0 {
		antReq.MaxTokens = 4096
	}

	// Enable extended thinking when budget is set
	if req.ThinkingBudget > 0 {
		antReq.Thinking = &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: req.ThinkingBudget,
		}
	}

	// Extract system prompt and convert messages
	for _, m := range req.Messages {
		if m.Role == domain.RoleSystem {
			antReq.System = m.Content
			continue
		}

		if m.Role == domain.RoleTool {
			// Tool results in Anthropic format
			antMsg := anthropicMessage{
				Role: "user",
				Content: []anthropicContent{
					{
						Type:      "tool_result",
						ToolUseID: extractToolCallID(m),
						Content:   m.Content,
					},
				},
			}
			antReq.Messages = append(antReq.Messages, antMsg)
			continue
		}

		antMsg := anthropicMessage{Role: m.Role}

		// Include thinking block if present (for conversation history replay)
		if m.Thinking != "" {
			antMsg.Content = append(antMsg.Content, anthropicContent{
				Type:     "thinking",
				Thinking: m.Thinking,
			})
		}

		if len(m.ToolCalls) > 0 {
			// Assistant message with tool calls
			if m.Content != "" {
				antMsg.Content = append(antMsg.Content, anthropicContent{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				antMsg.Content = append(antMsg.Content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
		} else {
			antMsg.Content = append(antMsg.Content, anthropicContent{Type: "text", Text: m.Content})
		}

		antReq.Messages = append(antReq.Messages, antMsg)
	}

	// Convert tools
	for _, t := range req.Tools {
		antReq.Tools = append(antReq.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	return antReq
}

func extractToolCallID(m domain.Message) string {
	if len(m.ToolCalls) > 0 {
		return m.ToolCalls[0].ID
	}
	return ""
}

func fromAnthropicResponse(resp anthropicResponse) *domain.ChatResponse {
	result := &domain.ChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Usage: domain.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		CreatedAt: time.Now(),
	}

	msg := domain.Message{
		Role:      domain.RoleAssistant,
		Timestamp: result.CreatedAt,
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content = block.Text
		case "thinking":
			msg.Thinking = block.Thinking
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}

	result.Message = msg
	return result
}
