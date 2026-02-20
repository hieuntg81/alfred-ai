package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/cronjob"
)

// CronTool exposes cron job CRUD to the LLM via function calling.
type CronTool struct {
	manager *cronjob.Manager
	logger  *slog.Logger
}

// NewCronTool creates a new cron tool backed by the given CronManager.
func NewCronTool(manager *cronjob.Manager, logger *slog.Logger) *CronTool {
	return &CronTool{manager: manager, logger: logger}
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Create, list, update, and delete scheduled cron jobs. Jobs execute agent messages on a schedule (one-shot, interval, or cron expression)."
}

func (t *CronTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["create", "list", "get", "update", "delete", "runs"],
					"description": "The operation to perform"
				},
				"job_id": {
					"type": "string",
					"description": "Job ID (required for get, update, delete, runs)"
				},
				"name": {
					"type": "string",
					"description": "Human-readable job name"
				},
				"schedule": {
					"type": "object",
					"properties": {
						"kind": {
							"type": "string",
							"enum": ["at", "every", "cron"],
							"description": "Schedule type: 'at' for one-shot, 'every' for interval, 'cron' for cron expression"
						},
						"at": {
							"type": "string",
							"description": "ISO 8601 timestamp for one-shot jobs (e.g. '2025-03-01T10:00:00Z')"
						},
						"every_ms": {
							"type": "integer",
							"description": "Interval in milliseconds for recurring jobs"
						},
						"expression": {
							"type": "string",
							"description": "Cron expression (e.g. '*/5 * * * *' for every 5 minutes)"
						}
					}
				},
				"message": {
					"type": "string",
					"description": "Message to send to the agent when the job fires"
				},
				"agent_id": {
					"type": "string",
					"description": "Target agent ID (optional, defaults to main agent)"
				},
				"channel": {
					"type": "string",
					"description": "Target channel (optional)"
				},
				"enabled": {
					"type": "boolean",
					"description": "Enable/disable the job (for update action)"
				},
				"limit": {
					"type": "integer",
					"description": "Max number of runs to return (for runs action, default 10)"
				}
			},
			"required": ["action"]
		}`),
	}
}

type cronParams struct {
	Action   string               `json:"action"`
	JobID    string               `json:"job_id"`
	Name     string               `json:"name"`
	Schedule *domain.CronSchedule `json:"schedule,omitempty"`
	Message  string               `json:"message"`
	AgentID  string               `json:"agent_id"`
	Channel  string               `json:"channel"`
	Enabled  *bool                `json:"enabled,omitempty"`
	Limit    int                  `json:"limit"`
}

func (t *CronTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.cron", t.logger, params,
		Dispatch(func(p cronParams) string { return p.Action }, ActionMap[cronParams]{
			"create": t.handleCreate,
			"list":   t.handleList,
			"get":    t.handleGet,
			"update": t.handleUpdate,
			"delete": t.handleDelete,
			"runs":   t.handleRuns,
		}),
	)
}

func (t *CronTool) handleCreate(ctx context.Context, p cronParams) (any, error) {
	if p.Schedule == nil {
		return nil, fmt.Errorf("'schedule' is required for create action")
	}
	if err := RequireField("message", p.Message); err != nil {
		return nil, err
	}

	job := domain.CronJob{
		Name:     p.Name,
		Schedule: *p.Schedule,
		Action: domain.CronAction{
			Kind:    "agent_run",
			AgentID: p.AgentID,
			Channel: p.Channel,
			Message: p.Message,
		},
	}

	return t.manager.Create(ctx, job)
}

func (t *CronTool) handleList(ctx context.Context, _ cronParams) (any, error) {
	return t.manager.List(ctx)
}

func (t *CronTool) handleGet(ctx context.Context, p cronParams) (any, error) {
	if err := RequireField("job_id", p.JobID); err != nil {
		return nil, err
	}
	return t.manager.Get(ctx, p.JobID)
}

func (t *CronTool) handleUpdate(ctx context.Context, p cronParams) (any, error) {
	if err := RequireField("job_id", p.JobID); err != nil {
		return nil, err
	}

	patch := cronjob.Patch{
		Schedule: p.Schedule,
		Enabled:  p.Enabled,
	}
	if p.Name != "" {
		patch.Name = &p.Name
	}
	if p.Message != "" {
		patch.Message = &p.Message
	}

	return t.manager.Update(ctx, p.JobID, patch)
}

func (t *CronTool) handleDelete(ctx context.Context, p cronParams) (any, error) {
	if err := RequireField("job_id", p.JobID); err != nil {
		return nil, err
	}
	if err := t.manager.Delete(ctx, p.JobID); err != nil {
		return nil, err
	}
	return map[string]bool{"deleted": true}, nil
}

func (t *CronTool) handleRuns(ctx context.Context, p cronParams) (any, error) {
	if err := RequireField("job_id", p.JobID); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}
	return t.manager.ListRuns(ctx, p.JobID, limit)
}
