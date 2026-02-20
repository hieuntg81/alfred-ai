package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/domain"

	"github.com/kaptinlin/jsonschema"
)

// LLMTaskTool delegates a structured task to an LLM and returns JSON-only output.
type LLMTaskTool struct {
	defaultProvider domain.LLMProvider
	registry        *llm.Registry
	logger          *slog.Logger
	config          LLMTaskConfig
}

// LLMTaskConfig holds configuration for the LLM task tool.
type LLMTaskConfig struct {
	AllowedModels []string      // e.g., ["openai/gpt-4o", "anthropic/claude-sonnet-4-5-20250929"]
	DefaultModel  string        // e.g., "gpt-4o"
	MaxTokens     int           // default max_tokens for LLM calls
	Timeout       time.Duration // timeout for LLM calls
	MaxPromptSize int           // max prompt length in bytes
	MaxInputSize  int           // max input payload size in bytes
}

// NewLLMTaskTool creates a new LLM task tool.
func NewLLMTaskTool(
	defaultProvider domain.LLMProvider,
	registry *llm.Registry,
	cfg LLMTaskConfig,
	logger *slog.Logger,
) *LLMTaskTool {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxPromptSize <= 0 {
		cfg.MaxPromptSize = 32 * 1024
	}
	if cfg.MaxInputSize <= 0 {
		cfg.MaxInputSize = 256 * 1024
	}
	return &LLMTaskTool{
		defaultProvider: defaultProvider,
		registry:        registry,
		logger:          logger,
		config:          cfg,
	}
}

func (t *LLMTaskTool) Name() string { return "llm_task" }
func (t *LLMTaskTool) Description() string {
	return "Run a structured LLM task that returns JSON-only output. Optionally validate the response against a JSON Schema. Supports provider and model override."
}

func (t *LLMTaskTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"prompt": {
					"type": "string",
					"description": "Task instruction for the LLM. Must clearly describe the desired JSON output."
				},
				"input": {
					"description": "Optional input payload (any JSON value) to include as context for the task."
				},
				"schema": {
					"type": "object",
					"description": "Optional JSON Schema to validate the LLM response against."
				},
				"provider": {
					"type": "string",
					"description": "Provider name override (e.g. 'openai', 'anthropic'). Must be a registered provider."
				},
				"model": {
					"type": "string",
					"description": "Model ID override (e.g. 'gpt-4o', 'claude-sonnet-4-5-20250929')."
				},
				"temperature": {
					"type": "number",
					"minimum": 0,
					"maximum": 2.0,
					"description": "Temperature override (0.0 - 2.0)."
				},
				"max_tokens": {
					"type": "integer",
					"minimum": 1,
					"description": "Max tokens override for the LLM response."
				},
				"timeout_ms": {
					"type": "integer",
					"minimum": 1,
					"description": "Timeout in milliseconds for the LLM call."
				}
			},
			"required": ["prompt"]
		}`),
	}
}

type llmTaskParams struct {
	Prompt      string          `json:"prompt"`
	Input       json.RawMessage `json:"input,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Provider    string          `json:"provider,omitempty"`
	Model       string          `json:"model,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	TimeoutMs   *int            `json:"timeout_ms,omitempty"`
}

func (t *LLMTaskTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.llm_task", t.logger, params,
		func(ctx context.Context, _ trace.Span, p llmTaskParams) (any, error) {
			prompt := strings.TrimSpace(p.Prompt)
			if err := RequireField("prompt", prompt); err != nil {
				return nil, err
			}

			// Input size limits
			if len(prompt) > t.config.MaxPromptSize {
				return nil, fmt.Errorf("prompt too large: %d bytes (max %d)", len(prompt), t.config.MaxPromptSize)
			}
			if len(p.Input) > t.config.MaxInputSize {
				return nil, fmt.Errorf("input too large: %d bytes (max %d)", len(p.Input), t.config.MaxInputSize)
			}

			// Resolve provider
			provider := t.defaultProvider
			providerName := t.defaultProvider.Name()
			if p.Provider != "" {
				resolved, err := t.registry.Get(p.Provider)
				if err != nil {
					return nil, fmt.Errorf("provider %q not found: %v", p.Provider, err)
				}
				provider = resolved
				providerName = p.Provider
			}

			// Resolve and validate model
			modelName := p.Model
			if modelName == "" {
				modelName = t.config.DefaultModel
			}

			if len(t.config.AllowedModels) > 0 && modelName != "" {
				modelKey := providerName + "/" + modelName
				if !t.isModelAllowed(modelKey) {
					return nil, fmt.Errorf("model %q not in allowlist; allowed: %s",
						modelKey, strings.Join(t.config.AllowedModels, ", "))
				}
			}

			// Build system prompt (JSON-only instruction)
			systemPrompt := "You are a JSON-only function. " +
				"Return ONLY a valid JSON value. " +
				"Do not wrap in markdown fences. " +
				"Do not include commentary. " +
				"Do not call tools."

			// Build user message with prompt + input
			var userContent strings.Builder
			userContent.WriteString("TASK:\n")
			userContent.WriteString(prompt)
			userContent.WriteString("\n")
			if len(p.Input) > 0 && !bytes.Equal(p.Input, []byte("null")) {
				userContent.WriteString("\nINPUT_JSON:\n")
				userContent.Write(p.Input)
				userContent.WriteString("\n")
			}

			// Build ChatRequest (no tools = JSON-only mode)
			maxTokens := t.config.MaxTokens
			if p.MaxTokens != nil && *p.MaxTokens > 0 {
				maxTokens = *p.MaxTokens
				if maxTokens > t.config.MaxTokens {
					maxTokens = t.config.MaxTokens
				}
			}

			temperature := 0.0
			if p.Temperature != nil {
				temperature = *p.Temperature
				if temperature < 0 {
					temperature = 0
				}
				if temperature > 2.0 {
					temperature = 2.0
				}
			}

			chatReq := domain.ChatRequest{
				Model: modelName,
				Messages: []domain.Message{
					{Role: domain.RoleSystem, Content: systemPrompt},
					{Role: domain.RoleUser, Content: userContent.String()},
				},
				MaxTokens:   maxTokens,
				Temperature: temperature,
			}

			// Apply timeout (capped at config timeout)
			timeout := t.config.Timeout
			if p.TimeoutMs != nil && *p.TimeoutMs > 0 {
				override := time.Duration(*p.TimeoutMs) * time.Millisecond
				if override > t.config.Timeout {
					override = t.config.Timeout
				}
				timeout = override
			}
			chatCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			// Call LLM
			t.logger.Info("llm_task calling LLM",
				"provider", providerName,
				"model", modelName,
				"prompt_len", len(prompt),
			)

			resp, err := provider.Chat(chatCtx, chatReq)
			if err != nil {
				return nil, fmt.Errorf("LLM call failed: %v", err)
			}

			// Extract and parse JSON from response
			raw := strings.TrimSpace(resp.Message.Content)
			if raw == "" {
				return nil, fmt.Errorf("LLM returned empty output")
			}

			raw = stripCodeFences(raw)

			var parsed any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				return nil, fmt.Errorf("LLM returned invalid JSON: %v\nRaw output: %s", err, truncate(raw, 500))
			}

			// Optional JSON Schema validation
			if len(p.Schema) > 0 && !bytes.Equal(p.Schema, []byte("null")) {
				if err := validateJSONSchema(p.Schema, parsed); err != nil {
					return nil, fmt.Errorf("LLM JSON did not match schema: %v", err)
				}
			}

			// Return formatted JSON
			formatted, err := json.MarshalIndent(parsed, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("failed to format JSON: %v", err)
			}

			return TextResult(string(formatted)), nil
		},
	)
}

// isModelAllowed checks if the model key is in the allowlist.
func (t *LLMTaskTool) isModelAllowed(modelKey string) bool {
	for _, allowed := range t.config.AllowedModels {
		if allowed == modelKey {
			return true
		}
	}
	return false
}

// validateJSONSchema validates parsed JSON against a JSON Schema.
func validateJSONSchema(schemaBytes json.RawMessage, data any) error {
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile([]byte(schemaBytes))
	if err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	result := schema.Validate(data)
	if !result.IsValid() {
		return fmt.Errorf("%s", result.Error())
	}
	return nil
}

// codeFenceRe matches markdown code fences wrapping JSON.
var codeFenceRe = regexp.MustCompile(`(?si)^` + "```" + `(?:json)?\s*(.*?)\s*` + "```" + `$`)

// stripCodeFences removes markdown code fences if the LLM wrapped its output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if m := codeFenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return s
}

// truncate shortens a string to maxLen bytes on a clean UTF-8 boundary,
// appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk runes to find the last boundary at or before maxLen bytes.
	end := 0
	for i := range s {
		if i > maxLen {
			break
		}
		end = i
	}
	return s[:end] + "..."
}
