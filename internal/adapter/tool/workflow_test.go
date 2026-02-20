package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase/workflow"
)

type stubCommandExecutor struct {
	stdout string
	stderr string
	err    error
}

func (s *stubCommandExecutor) Execute(_ context.Context, _ string, _ []string, _ string) (string, string, error) {
	return s.stdout, s.stderr, s.err
}

func newTestWorkflowTool(t *testing.T) *WorkflowTool {
	t.Helper()
	dataDir := t.TempDir()
	pipelineDir := filepath.Join(t.TempDir(), "pipelines")
	os.MkdirAll(pipelineDir, 0755)

	store, err := workflow.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	sb, err := security.NewSandbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	mgr := workflow.NewManager(
		store,
		workflow.ManagerConfig{
			PipelineDir:     pipelineDir,
			Timeout:         10 * time.Second,
			MaxOutput:       1024 * 1024,
			MaxRunning:      5,
			AllowedCommands: []string{"echo", "sh"},
		},
		sb,
		&stubCommandExecutor{stdout: "ok"},
		http.DefaultClient,
		nil,
		slog.Default(),
		nil, // toolExec
	)

	return NewWorkflowTool(mgr, slog.Default())
}

func execWorkflowTool(t *testing.T, wt *WorkflowTool, params any) *domain.ToolResult {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := wt.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	return result
}

func TestWorkflowToolName(t *testing.T) {
	wt := newTestWorkflowTool(t)
	if wt.Name() != "workflow" {
		t.Errorf("expected 'workflow', got %q", wt.Name())
	}
}

func TestWorkflowToolDescription(t *testing.T) {
	wt := newTestWorkflowTool(t)
	if wt.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestWorkflowToolSchema(t *testing.T) {
	wt := newTestWorkflowTool(t)
	schema := wt.Schema()
	if schema.Name != "workflow" {
		t.Errorf("schema name: expected 'workflow', got %q", schema.Name)
	}
	// Verify it's valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(schema.Parameters, &raw); err != nil {
		t.Fatalf("schema parameters is not valid JSON: %v", err)
	}
}

func TestWorkflowToolRunInline(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo", "args": []string{"hello"}},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var env workflowRunEnvelope
	if err := json.Unmarshal([]byte(result.Content), &env); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if env.Status != "completed" {
		t.Errorf("expected completed, got %s", env.Status)
	}
	if !env.OK {
		t.Error("expected ok=true")
	}
	if env.RunID == "" {
		t.Error("expected non-empty run_id")
	}
}

func TestWorkflowToolRunMissingPipelineAndSteps(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
	})
	if !result.IsError {
		t.Fatal("expected error when neither pipeline nor steps is provided")
	}
}

func TestWorkflowToolResumeApprove(t *testing.T) {
	wt := newTestWorkflowTool(t)

	// First: run a pipeline with an approval step.
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo"},
			{"id": "s2", "type": "approval", "message": "Continue?"},
			{"id": "s3", "type": "exec", "command": "echo"},
		},
	})
	if result.IsError {
		t.Fatalf("run error: %s", result.Content)
	}

	var env workflowRunEnvelope
	json.Unmarshal([]byte(result.Content), &env)
	if env.Status != "paused" {
		t.Fatalf("expected paused, got %s", env.Status)
	}
	if env.Approval == nil {
		t.Fatal("expected approval info")
	}

	// Resume with approval.
	approve := true
	result = execWorkflowTool(t, wt, map[string]any{
		"action":       "resume",
		"resume_token": env.Approval.ResumeToken,
		"approve":      approve,
	})
	if result.IsError {
		t.Fatalf("resume error: %s", result.Content)
	}

	json.Unmarshal([]byte(result.Content), &env)
	if env.Status != "completed" {
		t.Errorf("expected completed, got %s", env.Status)
	}
}

func TestWorkflowToolResumeDeny(t *testing.T) {
	wt := newTestWorkflowTool(t)

	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "approval", "message": "Continue?"},
		},
	})

	var env workflowRunEnvelope
	json.Unmarshal([]byte(result.Content), &env)

	deny := false
	result = execWorkflowTool(t, wt, map[string]any{
		"action":       "resume",
		"resume_token": env.Approval.ResumeToken,
		"approve":      deny,
	})
	if result.IsError {
		t.Fatalf("resume error: %s", result.Content)
	}

	json.Unmarshal([]byte(result.Content), &env)
	if env.Status != "denied" {
		t.Errorf("expected denied, got %s", env.Status)
	}
	if env.OK {
		t.Error("expected ok=false for denied")
	}
}

func TestWorkflowToolResumeMissingToken(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action":  "resume",
		"approve": true,
	})
	if !result.IsError {
		t.Fatal("expected error for missing resume_token")
	}
}

func TestWorkflowToolResumeMissingApprove(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action":       "resume",
		"resume_token": "some-token",
	})
	if !result.IsError {
		t.Fatal("expected error for missing approve")
	}
}

func TestWorkflowToolList(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "list",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var data workflowListEnvelope
	if err := json.Unmarshal([]byte(result.Content), &data); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if data.Pipelines == nil {
		t.Error("expected 'pipelines' in list result")
	}
	if data.Runs == nil {
		t.Error("expected 'runs' in list result")
	}
}

func TestWorkflowToolStatus(t *testing.T) {
	wt := newTestWorkflowTool(t)

	// Create a run first.
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo"},
		},
	})

	var env workflowRunEnvelope
	json.Unmarshal([]byte(result.Content), &env)

	// Check status.
	result = execWorkflowTool(t, wt, map[string]any{
		"action": "status",
		"run_id": env.RunID,
	})
	if result.IsError {
		t.Fatalf("status error: %s", result.Content)
	}

	// Verify status response doesn't contain embedded pipeline definition.
	var statusEnv workflowStatusEnvelope
	if err := json.Unmarshal([]byte(result.Content), &statusEnv); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if statusEnv.RunID == "" {
		t.Error("expected non-empty run_id in status")
	}
	if statusEnv.Status != "completed" {
		t.Errorf("expected completed, got %s", statusEnv.Status)
	}
}

func TestWorkflowToolStatusMissingRunID(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "status",
	})
	if !result.IsError {
		t.Fatal("expected error for missing run_id")
	}
}

func TestWorkflowToolUnknownAction(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "unknown",
	})
	if !result.IsError {
		t.Fatal("expected error for unknown action")
	}
}

func TestWorkflowToolInvalidParams(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result, err := wt.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("Execute should not return Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCompactEnvelopeRun(t *testing.T) {
	wt := newTestWorkflowTool(t)
	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo"},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var env workflowRunEnvelope
	if err := json.Unmarshal([]byte(result.Content), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify compact fields are present.
	if env.RunID == "" {
		t.Error("expected run_id")
	}
	if env.Status != "completed" {
		t.Errorf("expected completed, got %s", env.Status)
	}
	if !env.OK {
		t.Error("expected ok=true")
	}
	// Output should be the last step's output.
	if env.Output == nil {
		t.Error("expected output from last step")
	}
	// No approval or error for completed run.
	if env.Approval != nil {
		t.Error("expected no approval for completed run")
	}
	if env.Error != nil {
		t.Error("expected no error for completed run")
	}
}

func TestCompactEnvelopeList(t *testing.T) {
	wt := newTestWorkflowTool(t)

	// Run one to have at least one entry.
	execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo"},
		},
	})

	result := execWorkflowTool(t, wt, map[string]any{
		"action": "list",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var env workflowListEnvelope
	if err := json.Unmarshal([]byte(result.Content), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(env.Runs) < 1 {
		t.Error("expected at least 1 run in list")
	}
	// Verify run summary is compact (no pipeline definition embedded).
	for _, r := range env.Runs {
		if r.ID == "" {
			t.Error("expected run id in summary")
		}
		if r.Status == "" {
			t.Error("expected status in summary")
		}
	}
}

func TestToolCallStepInSchema(t *testing.T) {
	wt := newTestWorkflowTool(t)
	schema := wt.Schema()

	var raw map[string]any
	json.Unmarshal(schema.Parameters, &raw)
	props := raw["properties"].(map[string]any)
	stepsSchema := props["steps"].(map[string]any)
	items := stepsSchema["items"].(map[string]any)
	stepProps := items["properties"].(map[string]any)

	// Verify tool_call fields exist in schema.
	if _, ok := stepProps["tool_name"]; !ok {
		t.Error("expected 'tool_name' in step schema")
	}
	if _, ok := stepProps["tool_params"]; !ok {
		t.Error("expected 'tool_params' in step schema")
	}
	if _, ok := stepProps["fail_on_error"]; !ok {
		t.Error("expected 'fail_on_error' in step schema")
	}

	// Verify tool_call is in type enum.
	typeSchema := stepProps["type"].(map[string]any)
	enum := typeSchema["enum"].([]any)
	found := false
	for _, v := range enum {
		if v == "tool_call" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'tool_call' in step type enum")
	}

	// Verify timeout and max_output params exist at top level.
	if _, ok := props["timeout"]; !ok {
		t.Error("expected 'timeout' in top-level schema")
	}
	if _, ok := props["max_output"]; !ok {
		t.Error("expected 'max_output' in top-level schema")
	}
}

func TestStatusNoEmbeddedPipeline(t *testing.T) {
	wt := newTestWorkflowTool(t)

	result := execWorkflowTool(t, wt, map[string]any{
		"action": "run",
		"steps": []map[string]any{
			{"id": "s1", "type": "exec", "command": "echo"},
		},
	})

	var env workflowRunEnvelope
	json.Unmarshal([]byte(result.Content), &env)

	// Get status.
	result = execWorkflowTool(t, wt, map[string]any{
		"action": "status",
		"run_id": env.RunID,
	})
	if result.IsError {
		t.Fatalf("status error: %s", result.Content)
	}

	// The status response should NOT contain a "pipeline" key.
	var raw map[string]any
	json.Unmarshal([]byte(result.Content), &raw)
	if _, ok := raw["pipeline"]; ok {
		t.Error("status response should NOT contain embedded pipeline definition")
	}
}
