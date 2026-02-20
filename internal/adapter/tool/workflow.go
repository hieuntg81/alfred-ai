package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/workflow"
)

// --- compact response envelopes ---

type workflowRunEnvelope struct {
	OK       bool                  `json:"ok"`
	Status   string                `json:"status"`
	RunID    string                `json:"run_id"`
	Output   json.RawMessage       `json:"output,omitempty"`
	Approval *workflowApprovalInfo `json:"approval,omitempty"`
	Error    *string               `json:"error,omitempty"`
}

type workflowApprovalInfo struct {
	Message     string `json:"message"`
	ResumeToken string `json:"resume_token"`
}

type workflowPipelineSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type workflowRunSummary struct {
	ID           string    `json:"id"`
	PipelineName string    `json:"pipeline_name"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type workflowListEnvelope struct {
	Pipelines []workflowPipelineSummary `json:"pipelines"`
	Runs      []workflowRunSummary      `json:"runs"`
}

type workflowStatusEnvelope struct {
	RunID        string              `json:"run_id"`
	PipelineName string              `json:"pipeline_name"`
	Status       string              `json:"status"`
	Steps        []domain.StepResult `json:"steps"`
	Error        string              `json:"error,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// --- tool ---

// WorkflowTool exposes pipeline workflow operations to the LLM via function calling.
type WorkflowTool struct {
	manager *workflow.Manager
	logger  *slog.Logger
}

// NewWorkflowTool creates a new workflow tool backed by the given WorkflowManager.
func NewWorkflowTool(manager *workflow.Manager, logger *slog.Logger) *WorkflowTool {
	return &WorkflowTool{manager: manager, logger: logger}
}

func (t *WorkflowTool) Name() string { return "workflow" }
func (t *WorkflowTool) Description() string {
	return "Run, resume, list, and inspect pipeline workflows. Supports multi-step pipelines with exec, HTTP, transform, approval, and tool_call steps."
}

func (t *WorkflowTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["run", "resume", "list", "status"],
					"description": "The operation to perform"
				},
				"pipeline": {
					"type": "string",
					"description": "Pipeline name to run (for 'run' action)"
				},
				"steps": {
					"type": "array",
					"description": "Inline step definitions (for 'run' action, alternative to pipeline name)",
					"items": {
						"type": "object",
						"properties": {
							"id": {"type": "string"},
							"type": {"type": "string", "enum": ["exec", "http", "transform", "approval", "tool_call"]},
							"name": {"type": "string"},
							"command": {"type": "string"},
							"args": {"type": "array", "items": {"type": "string"}},
							"workdir": {"type": "string"},
							"url": {"type": "string"},
							"method": {"type": "string"},
							"headers": {"type": "object"},
							"body": {"type": "string"},
							"fail_on_error": {"type": "boolean"},
							"template": {"type": "string"},
							"message": {"type": "string"},
							"condition": {"type": "string"},
							"tool_name": {"type": "string"},
							"tool_params": {"type": "object"}
						},
						"required": ["id", "type"]
					}
				},
				"env": {
					"type": "object",
					"description": "Environment variables / pipeline args for the run",
					"additionalProperties": {"type": "string"}
				},
				"resume_token": {
					"type": "string",
					"description": "Resume token from a paused workflow (for 'resume' action)"
				},
				"approve": {
					"type": "boolean",
					"description": "Approve (true) or deny (false) a paused workflow (for 'resume' action)"
				},
				"run_id": {
					"type": "string",
					"description": "Workflow run ID (for 'status' action)"
				},
				"limit": {
					"type": "integer",
					"description": "Max results to return (for 'list' action, default 10)"
				},
				"timeout": {
					"type": "string",
					"description": "Per-call timeout override (e.g. '30s', '2m')"
				},
				"max_output": {
					"type": "integer",
					"description": "Per-call max output bytes override"
				}
			},
			"required": ["action"]
		}`),
	}
}

type workflowParams struct {
	Action      string            `json:"action"`
	Pipeline    string            `json:"pipeline"`
	Steps       []domain.Step     `json:"steps,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	ResumeToken string            `json:"resume_token"`
	Approve     *bool             `json:"approve,omitempty"`
	RunID       string            `json:"run_id"`
	Limit       int               `json:"limit"`
	Timeout     string            `json:"timeout,omitempty"`
	MaxOutput   int               `json:"max_output,omitempty"`
}

func (t *WorkflowTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.workflow", t.logger, params,
		Dispatch(func(p workflowParams) string { return p.Action }, ActionMap[workflowParams]{
			"run":    t.handleRun,
			"resume": t.handleResume,
			"list":   t.handleList,
			"status": t.handleStatus,
		}),
	)
}

func (t *WorkflowTool) handleRun(ctx context.Context, p workflowParams) (any, error) {
	opts := buildRunOptions(p)

	var run *domain.WorkflowRun
	var err error

	if p.Pipeline != "" {
		run, err = t.manager.Run(ctx, p.Pipeline, p.Env, opts)
	} else if len(p.Steps) > 0 {
		pipeline := domain.Pipeline{
			Name:  "inline",
			Steps: p.Steps,
		}
		run, err = t.manager.RunInline(ctx, pipeline, p.Env, opts)
	} else {
		return nil, fmt.Errorf("either 'pipeline' name or 'steps' array is required for run action")
	}

	if err != nil {
		return nil, err
	}
	return toRunEnvelope(run), nil
}

func (t *WorkflowTool) handleResume(ctx context.Context, p workflowParams) (any, error) {
	if err := RequireField("resume_token", p.ResumeToken); err != nil {
		return nil, err
	}
	if p.Approve == nil {
		return nil, fmt.Errorf("'approve' is required for resume action")
	}
	run, err := t.manager.Resume(ctx, p.ResumeToken, *p.Approve)
	if err != nil {
		return nil, err
	}
	return toRunEnvelope(run), nil
}

func (t *WorkflowTool) handleList(ctx context.Context, p workflowParams) (any, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}

	pipelines := t.manager.ListPipelines()
	runs, err := t.manager.ListRuns(ctx, limit)
	if err != nil {
		return nil, err
	}

	pSummaries := make([]workflowPipelineSummary, len(pipelines))
	for i, p := range pipelines {
		pSummaries[i] = workflowPipelineSummary{
			Name:        p.Name,
			Description: p.Description,
		}
	}

	rSummaries := make([]workflowRunSummary, len(runs))
	for i, r := range runs {
		rSummaries[i] = workflowRunSummary{
			ID:           r.ID,
			PipelineName: r.PipelineName,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt,
		}
	}

	return workflowListEnvelope{
		Pipelines: pSummaries,
		Runs:      rSummaries,
	}, nil
}

func (t *WorkflowTool) handleStatus(ctx context.Context, p workflowParams) (any, error) {
	if err := RequireField("run_id", p.RunID); err != nil {
		return nil, err
	}
	run, err := t.manager.GetRun(ctx, p.RunID)
	if err != nil {
		return nil, err
	}
	return workflowStatusEnvelope{
		RunID:        run.ID,
		PipelineName: run.PipelineName,
		Status:       run.Status,
		Steps:        run.Steps,
		Error:        run.Error,
		CreatedAt:    run.CreatedAt,
		UpdatedAt:    run.UpdatedAt,
	}, nil
}

// --- helpers ---

func toRunEnvelope(run *domain.WorkflowRun) workflowRunEnvelope {
	env := workflowRunEnvelope{
		OK:     run.Status != "failed" && run.Status != "denied",
		Status: run.Status,
		RunID:  run.ID,
	}

	// Attach last step output.
	if len(run.Steps) > 0 {
		last := run.Steps[len(run.Steps)-1]
		env.Output = last.Output
	}

	// Attach approval info if paused.
	if run.Status == "paused" && run.ResumeToken != "" {
		env.Approval = &workflowApprovalInfo{
			Message:     run.ApprovalMessage,
			ResumeToken: run.ResumeToken,
		}
	}

	// Attach error if failed.
	if run.Error != "" {
		env.Error = &run.Error
	}

	return env
}

func buildRunOptions(p workflowParams) *workflow.RunOptions {
	var opts workflow.RunOptions
	if p.Timeout != "" {
		if d, err := time.ParseDuration(p.Timeout); err == nil && d > 0 {
			opts.Timeout = d
		}
	}
	if p.MaxOutput > 0 {
		opts.MaxOutput = p.MaxOutput
	}
	if opts.Timeout == 0 && opts.MaxOutput == 0 {
		return nil
	}
	return &opts
}
