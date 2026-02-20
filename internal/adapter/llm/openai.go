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

// OpenAIProvider implements domain.LLMProvider for any OpenAI-compatible API.
type OpenAIProvider struct {
	name    string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// NewOpenAIProvider creates a provider with configured timeouts.
func NewOpenAIProvider(cfg config.ProviderConfig, logger *slog.Logger) *OpenAIProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIProvider{
		name:    cfg.Name,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  NewHTTPClient(cfg),
		logger:  logger,
	}
}

// Chat implements domain.LLMProvider.
func (p *OpenAIProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
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

	body, err := json.Marshal(toOpenAIRequest(req))
	if err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}

	respBody, err := doJSONRequest(ctx, p.client, p.baseURL+"/chat/completions", body, headers)
	if err != nil {
		tracer.RecordError(span, err)
		return nil, err
	}

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := fromOpenAIResponse(oaiResp)
	setUsageAttrs(span, result.Usage)
	tracer.SetOK(span)
	logChatCompleted(p.logger, p.name, result)

	return result, nil
}

// Name implements domain.LLMProvider.
func (p *OpenAIProvider) Name() string { return p.name }

// --- OpenAI API wire types ---

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
	Created int64          `json:"created"`
}

type openaiChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func toOpenAIRequest(req domain.ChatRequest) openaiRequest {
	msgs := make([]openaiMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		oaiMsg := openaiMessage{
			Role:    m.Role,
			Content: m.Content,
			Name:    m.Name,
		}

		// Handle tool result messages - map tool_call_id
		if m.Role == domain.RoleTool && len(m.ToolCalls) > 0 {
			// Tool result messages have the tool_call_id in ToolCalls[0].ID
			oaiMsg.ToolCallID = m.ToolCalls[0].ID
		}

		// Handle assistant messages with tool calls
		if len(m.ToolCalls) > 0 && m.Role != domain.RoleTool {
			oaiMsg.ToolCalls = make([]openaiToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				oaiMsg.ToolCalls[i] = openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openaiToolCallFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}

		msgs = append(msgs, oaiMsg)
	}

	oaiReq := openaiRequest{
		Model:    req.Model,
		Messages: msgs,
		Stream:   req.Stream,
	}

	if req.MaxTokens > 0 {
		oaiReq.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		oaiReq.Temperature = &req.Temperature
	}

	// Convert tools
	if len(req.Tools) > 0 {
		oaiReq.Tools = make([]openaiTool, len(req.Tools))
		for i, t := range req.Tools {
			oaiReq.Tools[i] = openaiTool{
				Type: "function",
				Function: openaiToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			}
		}
	}

	return oaiReq
}

// --- OpenAI streaming wire types ---

type openaiStreamChunk struct {
	ID      string               `json:"id"`
	Choices []openaiStreamChoice `json:"choices"`
	Usage   *openaiUsage         `json:"usage,omitempty"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   string           `json:"content,omitempty"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

// ChatStream implements domain.StreamingLLMProvider.
func (p *OpenAIProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	if req.Model == "" {
		req.Model = p.model
	}
	req.Stream = true

	oaiReq := toOpenAIRequest(req)
	oaiReq.Stream = true

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}

	httpResp, err := doStreamRequest(ctx, p.client, p.baseURL+"/chat/completions", body, headers)
	if err != nil {
		return nil, err
	}

	ch := parseSSEStream(ctx, httpResp.Body, func(data []byte) (*domain.StreamDelta, error) {
		var chunk openaiStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return nil, err
		}

		delta := &domain.StreamDelta{}
		if len(chunk.Choices) > 0 {
			c := chunk.Choices[0]
			delta.Content = c.Delta.Content
			for _, tc := range c.Delta.ToolCalls {
				delta.ToolCalls = append(delta.ToolCalls, domain.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				})
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				delta.Done = true
			}
		}
		if chunk.Usage != nil {
			delta.Usage = &domain.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		return delta, nil
	})

	return ch, nil
}

func fromOpenAIResponse(resp openaiResponse) *domain.ChatResponse {
	result := &domain.ChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Usage: domain.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		CreatedAt: time.Unix(resp.Created, 0),
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		msg := domain.Message{
			Role:      choice.Message.Role,
			Content:   choice.Message.Content,
			Name:      choice.Message.Name,
			Timestamp: result.CreatedAt,
		}

		// Convert tool calls
		if len(choice.Message.ToolCalls) > 0 {
			msg.ToolCalls = make([]domain.ToolCall, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				msg.ToolCalls[i] = domain.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				}
			}
		}

		result.Message = msg
	}

	return result
}
