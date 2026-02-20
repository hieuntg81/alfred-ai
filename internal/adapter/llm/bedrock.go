//go:build bedrock

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/infra/tracer"
)

// bedrockConverseAPI abstracts the Bedrock runtime methods for testability.
type bedrockConverseAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

// BedrockProvider implements domain.LLMProvider via the AWS Bedrock Converse API.
type BedrockProvider struct {
	name   string
	model  string
	client bedrockConverseAPI
	logger *slog.Logger
}

// NewBedrockProvider creates a Bedrock provider using the default AWS credential chain.
func NewBedrockProvider(cfg config.ProviderConfig, logger *slog.Logger) (*BedrockProvider, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(awsCfg)

	return &BedrockProvider{
		name:   cfg.Name,
		model:  cfg.Model,
		client: client,
		logger: logger,
	}, nil
}

// newBedrockProviderWithClient creates a BedrockProvider with an injected client (for testing).
func newBedrockProviderWithClient(name, model string, client bedrockConverseAPI, logger *slog.Logger) *BedrockProvider {
	return &BedrockProvider{
		name:   name,
		model:  model,
		client: client,
		logger: logger,
	}
}

// Chat implements domain.LLMProvider.
func (p *BedrockProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
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

	input := toBedrockConverseInput(req)

	output, err := p.client.Converse(ctx, input)
	if err != nil {
		tracer.RecordError(span, err)
		return nil, mapBedrockError(err)
	}

	result := fromBedrockConverseOutput(output, req.Model)
	setUsageAttrs(span, result.Usage)
	tracer.SetOK(span)
	logChatCompleted(p.logger, p.name, result)

	return result, nil
}

// ChatStream implements domain.StreamingLLMProvider.
func (p *BedrockProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamDelta, error) {
	if req.Model == "" {
		req.Model = p.model
	}

	input := toBedrockConverseStreamInput(req)

	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, mapBedrockError(err)
	}

	ch := make(chan domain.StreamDelta, 16)
	go func() {
		defer close(ch)
		stream := output.GetStream()
		defer stream.Close()

		for evt := range stream.Events() {
			delta := processBedrockStreamEvent(evt)
			if delta != nil {
				select {
				case ch <- *delta:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := stream.Err(); err != nil {
			select {
			case ch <- domain.StreamDelta{Done: true}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// Name implements domain.LLMProvider.
func (p *BedrockProvider) Name() string { return p.name }

// --- Bedrock request/response conversion ---

func toBedrockConverseInput(req domain.ChatRequest) *bedrockruntime.ConverseInput {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(req.Model),
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	input.InferenceConfig = &types.InferenceConfiguration{
		MaxTokens: aws.Int32(int32(maxTokens)),
	}
	if req.Temperature > 0 {
		input.InferenceConfig.Temperature = aws.Float32(float32(req.Temperature))
	}

	// Convert messages (extract system prompt)
	for _, m := range req.Messages {
		if m.Role == domain.RoleSystem {
			input.System = []types.SystemContentBlock{
				&types.SystemContentBlockMemberText{Value: m.Content},
			}
			continue
		}

		msg := toBedrockMessage(m)
		if msg != nil {
			input.Messages = append(input.Messages, *msg)
		}
	}

	// Convert tools
	if len(req.Tools) > 0 {
		input.ToolConfig = toBedrockToolConfig(req.Tools)
	}

	return input
}

func toBedrockConverseStreamInput(req domain.ChatRequest) *bedrockruntime.ConverseStreamInput {
	ci := toBedrockConverseInput(req)
	return &bedrockruntime.ConverseStreamInput{
		ModelId:         ci.ModelId,
		Messages:        ci.Messages,
		System:          ci.System,
		InferenceConfig: ci.InferenceConfig,
		ToolConfig:      ci.ToolConfig,
	}
}

func toBedrockMessage(m domain.Message) *types.Message {
	msg := &types.Message{}

	switch m.Role {
	case domain.RoleTool:
		msg.Role = types.ConversationRoleUser
		toolUseID := ""
		if len(m.ToolCalls) > 0 {
			toolUseID = m.ToolCalls[0].ID
		}
		msg.Content = []types.ContentBlock{
			&types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(toolUseID),
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{Value: m.Content},
					},
				},
			},
		}

	case domain.RoleAssistant:
		msg.Role = types.ConversationRoleAssistant
		if m.Content != "" {
			msg.Content = append(msg.Content, &types.ContentBlockMemberText{Value: m.Content})
		}
		for _, tc := range m.ToolCalls {
			var inputDoc map[string]interface{}
			if len(tc.Arguments) > 0 {
				json.Unmarshal(tc.Arguments, &inputDoc)
			}
			if inputDoc == nil {
				inputDoc = map[string]interface{}{}
			}
			toolUseBlock := types.ToolUseBlock{
				ToolUseId: aws.String(tc.ID),
				Name:      aws.String(tc.Name),
				Input:     document.NewLazyDocument(inputDoc),
			}
			msg.Content = append(msg.Content, &types.ContentBlockMemberToolUse{Value: toolUseBlock})
		}

	case domain.RoleUser:
		msg.Role = types.ConversationRoleUser
		msg.Content = []types.ContentBlock{
			&types.ContentBlockMemberText{Value: m.Content},
		}

	default:
		return nil
	}

	return msg
}

func toBedrockToolConfig(tools []domain.ToolSchema) *types.ToolConfiguration {
	var bedrockTools []types.Tool
	for _, t := range tools {
		var schema map[string]interface{}
		if len(t.Parameters) > 0 {
			json.Unmarshal(t.Parameters, &schema)
		}
		if schema == nil {
			schema = map[string]interface{}{"type": "object"}
		}

		bedrockTools = append(bedrockTools, &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(t.Name),
				Description: aws.String(t.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(schema),
				},
			},
		})
	}
	return &types.ToolConfiguration{Tools: bedrockTools}
}

func fromBedrockConverseOutput(output *bedrockruntime.ConverseOutput, model string) *domain.ChatResponse {
	now := time.Now()
	result := &domain.ChatResponse{
		Model:     model,
		CreatedAt: now,
	}

	if output.Usage != nil {
		result.Usage = domain.Usage{
			PromptTokens:     int(aws.ToInt32(output.Usage.InputTokens)),
			CompletionTokens: int(aws.ToInt32(output.Usage.OutputTokens)),
			TotalTokens:      int(aws.ToInt32(output.Usage.InputTokens)) + int(aws.ToInt32(output.Usage.OutputTokens)),
		}
	}

	msg := domain.Message{
		Role:      domain.RoleAssistant,
		Timestamp: now,
	}

	if outMsg, ok := output.Output.(*types.ConverseOutputMemberMessage); ok {
		for _, block := range outMsg.Value.Content {
			switch b := block.(type) {
			case *types.ContentBlockMemberText:
				msg.Content = b.Value
			case *types.ContentBlockMemberToolUse:
				args := marshalDocument(b.Value.Input)
				msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
					ID:        aws.ToString(b.Value.ToolUseId),
					Name:      aws.ToString(b.Value.Name),
					Arguments: args,
				})
			}
		}
	}

	result.Message = msg
	return result
}

// marshalDocument converts a Bedrock document.Interface to json.RawMessage.
func marshalDocument(doc document.Interface) json.RawMessage {
	if doc == nil {
		return json.RawMessage("{}")
	}
	var v interface{}
	if err := doc.UnmarshalSmithyDocument(&v); err != nil {
		return json.RawMessage("{}")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

func processBedrockStreamEvent(evt types.ConverseStreamOutput) *domain.StreamDelta {
	switch e := evt.(type) {
	case *types.ConverseStreamOutputMemberContentBlockDelta:
		switch d := e.Value.Delta.(type) {
		case *types.ContentBlockDeltaMemberText:
			return &domain.StreamDelta{Content: d.Value}
		case *types.ContentBlockDeltaMemberToolUse:
			return &domain.StreamDelta{Content: aws.ToString(d.Value.Input)}
		}
		return nil

	case *types.ConverseStreamOutputMemberContentBlockStart:
		if start, ok := e.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
			return &domain.StreamDelta{
				ToolCalls: []domain.ToolCall{{
					ID:   aws.ToString(start.Value.ToolUseId),
					Name: aws.ToString(start.Value.Name),
				}},
			}
		}
		return nil

	case *types.ConverseStreamOutputMemberMetadata:
		delta := &domain.StreamDelta{Done: true}
		if e.Value.Usage != nil {
			delta.Usage = &domain.Usage{
				PromptTokens:     int(aws.ToInt32(e.Value.Usage.InputTokens)),
				CompletionTokens: int(aws.ToInt32(e.Value.Usage.OutputTokens)),
				TotalTokens:      int(aws.ToInt32(e.Value.Usage.InputTokens)) + int(aws.ToInt32(e.Value.Usage.OutputTokens)),
			}
		}
		return delta

	case *types.ConverseStreamOutputMemberMessageStop:
		return &domain.StreamDelta{Done: true}

	default:
		return nil
	}
}

// --- Error mapping ---

func mapBedrockError(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		switch {
		case code == "ThrottlingException" || code == "TooManyRequestsException":
			return fmt.Errorf("%w: %s", domain.ErrRateLimit, msg)
		case code == "AccessDeniedException" || code == "UnrecognizedClientException":
			return fmt.Errorf("%w: %s", domain.ErrAuthInvalid, msg)
		case code == "ValidationException" && strings.Contains(msg, "too long"):
			return fmt.Errorf("%w: %s", domain.ErrContextOverflow, msg)
		case code == "ModelNotReadyException" || code == "ServiceUnavailableException" ||
			code == "InternalServerException":
			return fmt.Errorf("%w: %s", domain.ErrToolFailure, msg)
		}
	}

	return domain.WrapOp("bedrock", err)
}
