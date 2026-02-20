package skill

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"alfred-ai/internal/domain"
)

// SkillTool wraps a domain.Skill as a domain.Tool for LLM function-calling.
type SkillTool struct {
	skill  domain.Skill
	router domain.ModelRouter
	logger *slog.Logger
}

// SkillToolOption configures optional SkillTool dependencies.
type SkillToolOption func(*SkillTool)

// WithModelRouter sets the model router for preference-based LLM routing.
func WithModelRouter(r domain.ModelRouter) SkillToolOption {
	return func(t *SkillTool) { t.router = r }
}

// WithLogger sets the logger for the skill tool.
func WithLogger(l *slog.Logger) SkillToolOption {
	return func(t *SkillTool) { t.logger = l }
}

// NewSkillTool creates a tool from a skill.
func NewSkillTool(skill domain.Skill, opts ...SkillToolOption) *SkillTool {
	t := &SkillTool{skill: skill, logger: slog.Default()}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Name implements domain.Tool.
func (t *SkillTool) Name() string { return t.skill.Name }

// Description implements domain.Tool.
func (t *SkillTool) Description() string { return t.skill.Description }

// Schema implements domain.Tool.
func (t *SkillTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.skill.Name,
		Description: t.skill.Description,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"input": {
					"type": "string",
					"description": "The input to process with this skill"
				}
			},
			"required": ["input"]
		}`),
	}
}

// Execute implements domain.Tool.
func (t *SkillTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	var p struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &domain.ToolResult{
			Content: "invalid parameters: " + err.Error(),
			IsError: true,
		}, nil
	}

	if p.Input == "" {
		t.logger.Warn("skill executed with empty input", "skill", t.skill.Name)
	}

	// Simple template substitution: replace {{.input}} with actual input
	rendered := strings.ReplaceAll(t.skill.Template, "{{.input}}", p.Input)

	// If a model router is available and the skill has a preference, route to LLM.
	if t.router != nil && t.skill.ModelPreference != "" {
		provider, err := t.router.Route(t.skill.ModelPreference)
		if err != nil {
			t.logger.Warn("model routing failed, returning rendered template",
				"skill", t.skill.Name,
				"preference", t.skill.ModelPreference,
				"error", err,
			)
			return &domain.ToolResult{Content: rendered}, nil
		}

		resp, err := provider.Chat(ctx, domain.ChatRequest{
			Messages: []domain.Message{
				{Role: domain.RoleUser, Content: rendered},
			},
		})
		if err != nil {
			return &domain.ToolResult{
				Content:     "skill LLM call failed: " + err.Error(),
				IsError:     true,
				IsRetryable: true,
			}, nil
		}
		return &domain.ToolResult{Content: resp.Message.Content}, nil
	}

	return &domain.ToolResult{Content: rendered}, nil
}
