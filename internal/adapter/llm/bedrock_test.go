//go:build bedrock

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"

	"alfred-ai/internal/domain"
)

// --- Mock Bedrock client ---

type mockBedrockClient struct {
	converseFunc       func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	converseStreamFunc func(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

func (m *mockBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if m.converseFunc != nil {
		return m.converseFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockBedrockClient) ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error) {
	if m.converseStreamFunc != nil {
		return m.converseStreamFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}

// --- Tests ---

func TestBedrockChat(t *testing.T) {
	var receivedInput *bedrockruntime.ConverseInput

	mock := &mockBedrockClient{
		converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			receivedInput = params
			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role: types.ConversationRoleAssistant,
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Hello from Bedrock!"},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(5),
				},
			}, nil
		},
	}

	provider := newBedrockProviderWithClient("bedrock-test", "anthropic.claude-3-5-sonnet", mock, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are helpful."},
			{Role: domain.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Hello from Bedrock!" {
		t.Errorf("Content = %q", resp.Message.Content)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d", resp.Usage.TotalTokens)
	}

	// Verify request conversion
	if receivedInput == nil {
		t.Fatal("expected input to be captured")
	}
	if aws.ToString(receivedInput.ModelId) != "anthropic.claude-3-5-sonnet" {
		t.Errorf("ModelId = %q", aws.ToString(receivedInput.ModelId))
	}
	if len(receivedInput.System) != 1 {
		t.Fatalf("System len = %d, want 1", len(receivedInput.System))
	}
	if len(receivedInput.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system extracted)", len(receivedInput.Messages))
	}

	if provider.Name() != "bedrock-test" {
		t.Errorf("Name = %q", provider.Name())
	}
}

func TestBedrockChatWithToolUse(t *testing.T) {
	mock := &mockBedrockClient{
		converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			// Verify tools were sent
			if params.ToolConfig == nil || len(params.ToolConfig.Tools) != 1 {
				t.Errorf("expected 1 tool, got %v", params.ToolConfig)
			}

			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role: types.ConversationRoleAssistant,
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Let me read that."},
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("toolu_123"),
									Name:      aws.String("filesystem"),
									Input:     document.NewLazyDocument(map[string]interface{}{"path": "test.txt"}),
								},
							},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(20),
					OutputTokens: aws.Int32(15),
				},
			}, nil
		},
	}

	provider := newBedrockProviderWithClient("test", "model", mock, newTestLogger())

	resp, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read test.txt"},
		},
		Tools: []domain.ToolSchema{
			{Name: "filesystem", Description: "File ops", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Let me read that." {
		t.Errorf("Content = %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].ID != "toolu_123" {
		t.Errorf("ToolCall ID = %q", resp.Message.ToolCalls[0].ID)
	}
	if resp.Message.ToolCalls[0].Name != "filesystem" {
		t.Errorf("ToolCall Name = %q", resp.Message.ToolCalls[0].Name)
	}
}

func TestBedrockChatWithToolResult(t *testing.T) {
	var receivedInput *bedrockruntime.ConverseInput

	mock := &mockBedrockClient{
		converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			receivedInput = params
			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role: types.ConversationRoleAssistant,
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "The file contains hello"},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(30),
					OutputTokens: aws.Int32(10),
				},
			}, nil
		},
	}

	provider := newBedrockProviderWithClient("test", "model", mock, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "Read test.txt"},
			{
				Role: domain.RoleAssistant,
				ToolCalls: []domain.ToolCall{
					{ID: "toolu_abc", Name: "filesystem", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
				},
			},
			{
				Role:    domain.RoleTool,
				Content: "hello world",
				ToolCalls: []domain.ToolCall{
					{ID: "toolu_abc"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Verify tool result was properly converted
	if len(receivedInput.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(receivedInput.Messages))
	}
	// Tool result message
	toolMsg := receivedInput.Messages[2]
	if toolMsg.Role != types.ConversationRoleUser {
		t.Errorf("Tool result role = %q, want user", toolMsg.Role)
	}
	if len(toolMsg.Content) != 1 {
		t.Fatalf("Tool result content len = %d", len(toolMsg.Content))
	}
	toolResult, ok := toolMsg.Content[0].(*types.ContentBlockMemberToolResult)
	if !ok {
		t.Fatal("expected ContentBlockMemberToolResult")
	}
	if aws.ToString(toolResult.Value.ToolUseId) != "toolu_abc" {
		t.Errorf("ToolUseId = %q", aws.ToString(toolResult.Value.ToolUseId))
	}
}

func TestBedrockChatDefaultModel(t *testing.T) {
	var receivedModel string

	mock := &mockBedrockClient{
		converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			receivedModel = aws.ToString(params.ModelId)
			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role:    types.ConversationRoleAssistant,
						Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "ok"}},
					},
				},
				Usage: &types.TokenUsage{InputTokens: aws.Int32(1), OutputTokens: aws.Int32(1)},
			}, nil
		},
	}

	provider := newBedrockProviderWithClient("test", "anthropic.claude-3-5-sonnet", mock, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedModel != "anthropic.claude-3-5-sonnet" {
		t.Errorf("Model = %q, want default", receivedModel)
	}
}

func TestBedrockChatDefaultMaxTokens(t *testing.T) {
	var receivedMaxTokens int32

	mock := &mockBedrockClient{
		converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
			if params.InferenceConfig != nil && params.InferenceConfig.MaxTokens != nil {
				receivedMaxTokens = *params.InferenceConfig.MaxTokens
			}
			return &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role:    types.ConversationRoleAssistant,
						Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "ok"}},
					},
				},
				Usage: &types.TokenUsage{InputTokens: aws.Int32(1), OutputTokens: aws.Int32(1)},
			}, nil
		},
	}

	provider := newBedrockProviderWithClient("test", "model", mock, newTestLogger())

	_, err := provider.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedMaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", receivedMaxTokens)
	}
}

// --- Error mapping tests ---

type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) Error() string            { return e.message }
func (e *mockAPIError) ErrorCode() string         { return e.code }
func (e *mockAPIError) ErrorMessage() string      { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

func TestBedrockErrorMapping(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name:    "throttling",
			err:     &mockAPIError{code: "ThrottlingException", message: "rate limited"},
			wantErr: domain.ErrRateLimit,
		},
		{
			name:    "too many requests",
			err:     &mockAPIError{code: "TooManyRequestsException", message: "too many"},
			wantErr: domain.ErrRateLimit,
		},
		{
			name:    "access denied",
			err:     &mockAPIError{code: "AccessDeniedException", message: "no access"},
			wantErr: domain.ErrAuthInvalid,
		},
		{
			name:    "validation context too long",
			err:     &mockAPIError{code: "ValidationException", message: "input is too long"},
			wantErr: domain.ErrContextOverflow,
		},
		{
			name:    "internal server error",
			err:     &mockAPIError{code: "InternalServerException", message: "server error"},
			wantErr: domain.ErrToolFailure,
		},
		{
			name:    "service unavailable",
			err:     &mockAPIError{code: "ServiceUnavailableException", message: "unavailable"},
			wantErr: domain.ErrToolFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBedrockClient{
				converseFunc: func(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
					return nil, tt.err
				},
			}

			provider := newBedrockProviderWithClient("test", "model", mock, newTestLogger())

			_, err := provider.Chat(context.Background(), domain.ChatRequest{
				Messages: []domain.Message{{Role: domain.RoleUser, Content: "test"}},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestBedrockRequestConversion(t *testing.T) {
	req := domain.ChatRequest{
		Model: "anthropic.claude-3-haiku",
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "Be helpful"},
			{Role: domain.RoleUser, Content: "Hello"},
		},
		MaxTokens:   2048,
		Temperature: 0.7,
	}

	input := toBedrockConverseInput(req)

	if aws.ToString(input.ModelId) != "anthropic.claude-3-haiku" {
		t.Errorf("ModelId = %q", aws.ToString(input.ModelId))
	}
	if len(input.System) != 1 {
		t.Fatalf("System len = %d", len(input.System))
	}
	if len(input.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system extracted)", len(input.Messages))
	}
	if aws.ToInt32(input.InferenceConfig.MaxTokens) != 2048 {
		t.Errorf("MaxTokens = %d", aws.ToInt32(input.InferenceConfig.MaxTokens))
	}
	if aws.ToFloat32(input.InferenceConfig.Temperature) != 0.7 {
		t.Errorf("Temperature = %f", aws.ToFloat32(input.InferenceConfig.Temperature))
	}
}

func TestBedrockStreamConversion(t *testing.T) {
	// Test content_block_delta with text
	textDelta := &types.ConverseStreamOutputMemberContentBlockDelta{
		Value: types.ContentBlockDeltaEvent{
			ContentBlockIndex: aws.Int32(0),
			Delta:             &types.ContentBlockDeltaMemberText{Value: "Hello"},
		},
	}
	delta := processBedrockStreamEvent(textDelta)
	if delta == nil || delta.Content != "Hello" {
		t.Errorf("text delta: got %+v", delta)
	}

	// Test content_block_start with tool_use
	toolStart := &types.ConverseStreamOutputMemberContentBlockStart{
		Value: types.ContentBlockStartEvent{
			ContentBlockIndex: aws.Int32(1),
			Start: &types.ContentBlockStartMemberToolUse{
				Value: types.ToolUseBlockStart{
					ToolUseId: aws.String("tool_1"),
					Name:      aws.String("fs"),
				},
			},
		},
	}
	delta = processBedrockStreamEvent(toolStart)
	if delta == nil || len(delta.ToolCalls) != 1 {
		t.Fatalf("tool start: got %+v", delta)
	}
	if delta.ToolCalls[0].ID != "tool_1" {
		t.Errorf("ToolCall ID = %q", delta.ToolCalls[0].ID)
	}

	// Test metadata with usage
	metadata := &types.ConverseStreamOutputMemberMetadata{
		Value: types.ConverseStreamMetadataEvent{
			Usage: &types.TokenUsage{
				InputTokens:  aws.Int32(10),
				OutputTokens: aws.Int32(20),
			},
		},
	}
	delta = processBedrockStreamEvent(metadata)
	if delta == nil || !delta.Done {
		t.Fatalf("metadata: got %+v", delta)
	}
	if delta.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d", delta.Usage.PromptTokens)
	}
	if delta.Usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d", delta.Usage.TotalTokens)
	}

	// Test message_stop
	stop := &types.ConverseStreamOutputMemberMessageStop{
		Value: types.MessageStopEvent{},
	}
	delta = processBedrockStreamEvent(stop)
	if delta == nil || !delta.Done {
		t.Errorf("message stop: got %+v", delta)
	}
}
