package domain

import (
	"context"
	"encoding/json"
	"time"
)

// PipelineArg defines an input parameter for a pipeline.
type PipelineArg struct {
	Default     string `json:"default,omitempty" yaml:"default,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Pipeline is a workflow definition loaded from YAML or provided inline.
type Pipeline struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Steps       []Step                 `json:"steps" yaml:"steps"`
	Timeout     time.Duration          `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Env         map[string]string      `json:"env,omitempty" yaml:"env,omitempty"`
	Args        map[string]PipelineArg `json:"args,omitempty" yaml:"args,omitempty"`
}

// Step is a single unit of work inside a Pipeline.
type Step struct {
	ID        string        `json:"id" yaml:"id"`
	Type      string        `json:"type" yaml:"type"` // "exec", "http", "transform", "approval", "tool_call"
	Name      string        `json:"name,omitempty" yaml:"name,omitempty"`
	Timeout   time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Condition string        `json:"condition,omitempty" yaml:"condition,omitempty"` // Go text/template bool expression

	// exec step fields
	Command string   `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string `json:"args,omitempty" yaml:"args,omitempty"`
	WorkDir string   `json:"workdir,omitempty" yaml:"workdir,omitempty"`

	// http step fields
	URL         string            `json:"url,omitempty" yaml:"url,omitempty"`
	Method      string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body        string            `json:"body,omitempty" yaml:"body,omitempty"`
	FailOnError bool              `json:"fail_on_error,omitempty" yaml:"fail_on_error,omitempty"`

	// transform step fields
	Template string `json:"template,omitempty" yaml:"template,omitempty"`

	// approval step fields
	Message string `json:"message,omitempty" yaml:"message,omitempty"`

	// tool_call step fields
	ToolName   string          `json:"tool_name,omitempty" yaml:"tool_name,omitempty"`
	ToolParams json.RawMessage `json:"tool_params,omitempty" yaml:"tool_params,omitempty"`
}

// WorkflowRun tracks the runtime state of a single pipeline execution.
type WorkflowRun struct {
	ID           string            `json:"id"`
	PipelineName string            `json:"pipeline_name"`
	Status       string            `json:"status"` // "running", "paused", "completed", "failed", "denied"
	Steps        []StepResult      `json:"steps"`
	CurrentStep  int               `json:"current_step"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Error        string            `json:"error,omitempty"`
	Pipeline     Pipeline          `json:"pipeline"`
	Env          map[string]string `json:"env,omitempty"`

	// Effective per-run overrides (clamped to config max).
	EffectiveTimeout   time.Duration `json:"effective_timeout,omitempty"`
	EffectiveMaxOutput int           `json:"effective_max_output,omitempty"`

	// Approval state (populated when Status == "paused")
	ApprovalMessage string `json:"approval_message,omitempty"`
	ResumeToken     string `json:"resume_token,omitempty"`
}

// StepResult records the outcome of executing a single step.
type StepResult struct {
	StepID   string          `json:"step_id"`
	Status   string          `json:"status"` // "completed", "failed", "skipped"
	Output   json.RawMessage `json:"output"`
	Error    string          `json:"error,omitempty"`
	Duration time.Duration   `json:"duration"`
}

// WorkflowStore persists workflow runs for resumability.
type WorkflowStore interface {
	SaveRun(ctx context.Context, run WorkflowRun) error
	GetRun(ctx context.Context, id string) (*WorkflowRun, error)
	ListRuns(ctx context.Context, limit int) ([]WorkflowRun, error)
	DeleteRun(ctx context.Context, id string) error
	GetRunByToken(ctx context.Context, token string) (*WorkflowRun, error)
}
