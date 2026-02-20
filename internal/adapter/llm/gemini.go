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

// GeminiProvider implements domain.LLMProvider for the Google Gemini API.
type GeminiProvider struct {
	name    string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// NewGeminiProvider creates a provider for the Google Gemini API.
func NewGeminiProvider(cfg config.ProviderConfig, logger *slog.Logger) *GeminiProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	return &GeminiProvider{
		name:    cfg.Name,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  NewHTTPClient(cfg),
		logger:  logger,
	}
}

// Chat implements domain.LLMProvider.
func (p *GeminiProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
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

	body, err := json.Marshal(toGeminiRequest(req))
	if err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, req.Model, p.apiKey)

	respBody, err := doJSONRequest(ctx, p.client, url, body, nil)
	if err != nil {
		tracer.RecordError(span, err)
		return nil, err
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		tracer.RecordError(span, err)
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := fromGeminiResponse(gemResp)
	setUsageAttrs(span, result.Usage)
	tracer.SetOK(span)
	logChatCompleted(p.logger, p.name, result)

	return result, nil
}

// Name implements domain.LLMProvider.
func (p *GeminiProvider) Name() string { return p.name }

// --- Gemini API wire types ---

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	Tools             []geminiTool    `json:"tools,omitempty"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFuncResponse struct {
	Name     string         `json:"name"`
	Response geminiResponse `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations"`
}

type geminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Gemini streaming wire types ---

type geminiStreamChunk = geminiResponse // same shape as non-streaming

// ChatStream implements domain.StreamingLLMProvider.
func (p *GeminiProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	if req.Model == "" {
		req.Model = p.model
	}

	gemReq := toGeminiRequest(req)

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		p.baseURL, req.Model, p.apiKey)

	httpResp, err := doStreamRequest(ctx, p.client, url, body, nil)
	if err != nil {
		return nil, err
	}

	ch := parseSSEStream(ctx, httpResp.Body, func(data []byte) (*domain.StreamDelta, error) {
		var chunk geminiStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return nil, err
		}

		delta := &domain.StreamDelta{}
		if len(chunk.Candidates) > 0 {
			for _, part := range chunk.Candidates[0].Content.Parts {
				if part.FunctionCall != nil {
					delta.ToolCalls = append(delta.ToolCalls, domain.ToolCall{
						ID:        fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
						Name:      part.FunctionCall.Name,
						Arguments: part.FunctionCall.Args,
					})
				} else if part.Text != "" {
					delta.Content += part.Text
				}
			}
		}
		if chunk.UsageMetadata != nil {
			delta.Usage = &domain.Usage{
				PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
				CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
			}
		}
		return delta, nil
	})

	return ch, nil
}

func toGeminiRequest(req domain.ChatRequest) geminiRequest {
	gemReq := geminiRequest{}

	for _, m := range req.Messages {
		if m.Role == domain.RoleSystem {
			gemReq.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}

		role := "user"
		if m.Role == domain.RoleAssistant {
			role = "model"
		}

		gc := geminiContent{Role: role}

		if m.Role == domain.RoleTool {
			// Tool result in Gemini format
			gc.Role = "function"
			gc.Parts = []geminiPart{
				{
					FunctionResponse: &geminiFuncResponse{
						Name: m.Name,
						Response: geminiResponse{
							Candidates: []geminiCandidate{
								{Content: geminiContent{Parts: []geminiPart{{Text: m.Content}}}},
							},
						},
					},
				},
			}
		} else if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				gc.Parts = append(gc.Parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: tc.Name,
						Args: tc.Arguments,
					},
				})
			}
		} else {
			gc.Parts = []geminiPart{{Text: m.Content}}
		}

		gemReq.Contents = append(gemReq.Contents, gc)
	}

	// Convert tools
	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		gemReq.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}

	return gemReq
}

func fromGeminiResponse(resp geminiResponse) *domain.ChatResponse {
	result := &domain.ChatResponse{
		CreatedAt: time.Now(),
	}

	if resp.UsageMetadata != nil {
		result.Usage = domain.Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	msg := domain.Message{
		Role:      domain.RoleAssistant,
		Timestamp: result.CreatedAt,
	}

	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.FunctionCall != nil {
				msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
					ID:        fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				})
			} else if part.Text != "" {
				msg.Content = part.Text
			}
		}
	}

	result.Message = msg
	return result
}
