package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
)

// CommandExecutor abstracts shell command execution.
// Defined here to avoid import cycles with adapter/tool.ShellBackend.
type CommandExecutor interface {
	Execute(ctx context.Context, command string, args []string, workDir string) (stdout, stderr string, err error)
}

// RunOptions holds per-call overrides for workflow execution.
type RunOptions struct {
	Timeout   time.Duration
	MaxOutput int
}

// ManagerConfig holds configuration for the workflow engine.
type ManagerConfig struct {
	PipelineDir             string
	Timeout                 time.Duration
	MaxOutput               int
	MaxRunning              int
	AllowedCommands         []string
	WorkflowAllowedCommands []string // if set, overrides AllowedCommands for exec steps
}

// blockedTools prevents recursive tool_call invocations.
var blockedTools = map[string]bool{"workflow": true}

// Manager orchestrates pipeline loading, execution, and resumption.
type Manager struct {
	store      domain.WorkflowStore
	cfg        ManagerConfig
	sandbox    *security.Sandbox
	shell      CommandExecutor
	httpClient *http.Client
	bus        domain.EventBus
	logger     *slog.Logger
	toolExec   domain.ToolExecutor // optional; nil = tool_call steps rejected

	pipelines atomic.Value // map[string]domain.Pipeline
	running   atomic.Int32
}

// NewManager creates a new workflow engine.
func NewManager(
	store domain.WorkflowStore,
	cfg ManagerConfig,
	sandbox *security.Sandbox,
	shell CommandExecutor,
	httpClient *http.Client,
	bus domain.EventBus,
	logger *slog.Logger,
	toolExec domain.ToolExecutor,
) *Manager {
	m := &Manager{
		store:      store,
		cfg:        cfg,
		sandbox:    sandbox,
		shell:      shell,
		httpClient: httpClient,
		bus:        bus,
		logger:     logger,
		toolExec:   toolExec,
	}
	m.pipelines.Store(make(map[string]domain.Pipeline))
	return m
}

// LoadPipelines reads YAML pipeline definitions from the configured directory.
func (m *Manager) LoadPipelines() error {
	dir := m.cfg.PipelineDir
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Debug("pipeline directory does not exist", "dir", dir)
			return nil
		}
		return fmt.Errorf("read pipeline dir: %w", err)
	}

	loaded := make(map[string]domain.Pipeline)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			m.logger.Warn("skip unreadable pipeline file", "file", entry.Name(), "error", err)
			continue
		}

		var p domain.Pipeline
		if err := yaml.Unmarshal(data, &p); err != nil {
			m.logger.Warn("skip invalid pipeline file", "file", entry.Name(), "error", err)
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		if err := validatePipeline(p); err != nil {
			m.logger.Warn("skip invalid pipeline", "file", entry.Name(), "error", err)
			continue
		}

		loaded[p.Name] = p
	}

	m.pipelines.Store(loaded)
	m.logger.Info("pipelines loaded", "count", len(loaded))
	return nil
}

// ListPipelines returns all loaded pipeline definitions.
func (m *Manager) ListPipelines() []domain.Pipeline {
	pm := m.pipelines.Load().(map[string]domain.Pipeline)
	result := make([]domain.Pipeline, 0, len(pm))
	for _, p := range pm {
		result = append(result, p)
	}
	return result
}

// Run starts a named pipeline.
func (m *Manager) Run(ctx context.Context, pipelineName string, env map[string]string, opts *RunOptions) (*domain.WorkflowRun, error) {
	pm := m.pipelines.Load().(map[string]domain.Pipeline)
	p, ok := pm[pipelineName]
	if !ok {
		return nil, domain.NewSubSystemError("pipeline", "Manager.Run", domain.ErrNotFound, pipelineName)
	}
	return m.executePipeline(ctx, p, env, opts)
}

// RunInline starts a pipeline from inline step definitions.
func (m *Manager) RunInline(ctx context.Context, pipeline domain.Pipeline, env map[string]string, opts *RunOptions) (*domain.WorkflowRun, error) {
	if err := validatePipeline(pipeline); err != nil {
		return nil, err
	}
	return m.executePipeline(ctx, pipeline, env, opts)
}

// Resume continues a paused workflow.
func (m *Manager) Resume(ctx context.Context, token string, approve bool) (*domain.WorkflowRun, error) {
	run, err := m.store.GetRunByToken(ctx, token)
	if err != nil {
		return nil, domain.NewSubSystemError("workflow", "Manager.Resume", domain.ErrInvalidInput, err.Error())
	}

	if !approve {
		run.Status = "denied"
		run.ResumeToken = ""
		run.UpdatedAt = time.Now()
		if err := m.store.SaveRun(ctx, *run); err != nil {
			m.logger.Warn("failed to save denied run", "run_id", run.ID, "error", err)
		}
		return run, nil
	}

	// Clear approval state and continue execution.
	run.ResumeToken = ""
	run.ApprovalMessage = ""
	run.Status = "running"
	run.CurrentStep++ // move past the approval step
	run.UpdatedAt = time.Now()

	m.emitEvent(ctx, domain.EventWorkflowResumed, map[string]string{"run_id": run.ID})

	return m.continueExecution(ctx, run)
}

// GetRun returns a workflow run by ID.
func (m *Manager) GetRun(ctx context.Context, runID string) (*domain.WorkflowRun, error) {
	return m.store.GetRun(ctx, runID)
}

// ListRuns returns recent workflow runs.
func (m *Manager) ListRuns(ctx context.Context, limit int) ([]domain.WorkflowRun, error) {
	return m.store.ListRuns(ctx, limit)
}

// --- internal execution ---

func (m *Manager) executePipeline(ctx context.Context, pipeline domain.Pipeline, env map[string]string, opts *RunOptions) (*domain.WorkflowRun, error) {
	if int(m.running.Load()) >= m.cfg.MaxRunning {
		return nil, domain.NewSubSystemError("workflow", "Manager.executePipeline", domain.ErrLimitReached,
			fmt.Sprintf("%d/%d", m.running.Load(), m.cfg.MaxRunning))
	}
	m.running.Add(1)
	defer m.running.Add(-1)

	// Merge pipeline args defaults with caller env.
	mergedEnv := make(map[string]string)
	for name, arg := range pipeline.Args {
		if arg.Default != "" {
			mergedEnv[name] = arg.Default
		}
	}
	for k, v := range pipeline.Env {
		mergedEnv[k] = v
	}
	for k, v := range env {
		mergedEnv[k] = v
	}
	if err := validatePipelineArgs(pipeline, mergedEnv); err != nil {
		return nil, err
	}

	// Compute effective timeout and max output (clamp to config max).
	effectiveTimeout := m.cfg.Timeout
	if pipeline.Timeout > 0 {
		effectiveTimeout = pipeline.Timeout
	}
	if opts != nil && opts.Timeout > 0 && opts.Timeout < effectiveTimeout {
		effectiveTimeout = opts.Timeout
	}
	effectiveMaxOutput := m.cfg.MaxOutput
	if opts != nil && opts.MaxOutput > 0 && opts.MaxOutput < m.cfg.MaxOutput {
		effectiveMaxOutput = opts.MaxOutput
	}

	now := time.Now()
	run := &domain.WorkflowRun{
		ID:                 generateWorkflowID(now),
		PipelineName:       pipeline.Name,
		Status:             "running",
		Steps:              make([]domain.StepResult, 0, len(pipeline.Steps)),
		CurrentStep:        0,
		CreatedAt:          now,
		UpdatedAt:          now,
		Pipeline:           pipeline,
		Env:                mergedEnv,
		EffectiveTimeout:   effectiveTimeout,
		EffectiveMaxOutput: effectiveMaxOutput,
	}

	m.emitEvent(ctx, domain.EventWorkflowStarted, map[string]string{
		"run_id":   run.ID,
		"pipeline": pipeline.Name,
	})

	return m.continueExecution(ctx, run)
}

func (m *Manager) continueExecution(ctx context.Context, run *domain.WorkflowRun) (*domain.WorkflowRun, error) {
	timeout := run.EffectiveTimeout
	if timeout <= 0 {
		timeout = m.cfg.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	steps := run.Pipeline.Steps
	for i := run.CurrentStep; i < len(steps); i++ {
		step := steps[i]
		run.CurrentStep = i

		// Evaluate condition.
		if step.Condition != "" {
			ok, err := m.evaluateCondition(step.Condition, run.Steps, run.Env)
			if err != nil {
				m.logger.Warn("condition evaluation failed", "step", step.ID, "error", err)
			}
			if !ok {
				run.Steps = append(run.Steps, domain.StepResult{
					StepID: step.ID,
					Status: "skipped",
					Output: json.RawMessage(`null`),
				})
				continue
			}
		}

		result, err := m.executeStep(ctx, run, step)
		if err != nil {
			run.Status = "failed"
			run.Error = err.Error()
			run.UpdatedAt = time.Now()
			m.store.SaveRun(ctx, *run)
			m.emitEvent(ctx, domain.EventWorkflowFailed, map[string]string{
				"run_id": run.ID,
				"error":  run.Error,
			})
			return run, nil
		}

		run.Steps = append(run.Steps, *result)

		// If this was an approval step, execution pauses.
		if run.Status == "paused" {
			m.store.SaveRun(ctx, *run)
			m.emitEvent(ctx, domain.EventWorkflowPaused, map[string]string{
				"run_id":  run.ID,
				"message": run.ApprovalMessage,
			})
			return run, nil
		}
	}

	run.Status = "completed"
	run.UpdatedAt = time.Now()
	m.store.SaveRun(ctx, *run)
	m.emitEvent(ctx, domain.EventWorkflowCompleted, map[string]string{
		"run_id":   run.ID,
		"pipeline": run.PipelineName,
	})
	return run, nil
}

func (m *Manager) executeStep(ctx context.Context, run *domain.WorkflowRun, step domain.Step) (*domain.StepResult, error) {
	stepTimeout := step.Timeout
	if stepTimeout <= 0 {
		stepTimeout = run.EffectiveTimeout
	}
	if stepTimeout <= 0 {
		stepTimeout = m.cfg.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()

	start := time.Now()
	args := run.Env
	maxOutput := run.EffectiveMaxOutput

	switch step.Type {
	case "exec":
		return m.executeExecStep(ctx, step, run.Steps, args, maxOutput, start)
	case "http":
		return m.executeHTTPStep(ctx, step, run.Steps, args, maxOutput, start)
	case "transform":
		return m.executeTransformStep(step, run.Steps, args, maxOutput, start)
	case "approval":
		return m.executeApprovalStep(run, step, run.Steps, args, start)
	case "tool_call":
		return m.executeToolCallStep(ctx, step, run.Steps, args, maxOutput, start)
	default:
		return nil, domain.NewSubSystemError("workflow", "Manager.executeStep", domain.ErrInvalidInput,
			fmt.Sprintf("unknown step type %q", step.Type))
	}
}

func (m *Manager) executeExecStep(ctx context.Context, step domain.Step, prev []domain.StepResult, args map[string]string, maxOutput int, start time.Time) (*domain.StepResult, error) {
	if err := m.validateCommand(step.Command); err != nil {
		return nil, err
	}

	workDir := m.sandbox.Root()
	if step.WorkDir != "" {
		resolved, err := m.sandbox.ValidatePath(step.WorkDir)
		if err != nil {
			return nil, err
		}
		workDir = resolved
	}

	// Resolve template references in args.
	resolvedArgs := make([]string, len(step.Args))
	for i, a := range step.Args {
		resolvedArgs[i] = m.resolveTemplate(a, prev, args)
	}

	stdout, stderr, err := m.shell.Execute(ctx, step.Command, resolvedArgs, workDir)

	output := stdout
	if stderr != "" {
		output += "\nSTDERR:\n" + stderr
	}

	output = m.truncateOutput(output, maxOutput)

	if err != nil {
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   toJSON(output),
			Error:    err.Error(),
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeExecStep", domain.ErrToolFailure, err.Error())
	}

	return &domain.StepResult{
		StepID:   step.ID,
		Status:   "completed",
		Output:   toJSON(output),
		Duration: time.Since(start),
	}, nil
}

func (m *Manager) executeHTTPStep(ctx context.Context, step domain.Step, prev []domain.StepResult, args map[string]string, maxOutput int, start time.Time) (*domain.StepResult, error) {
	// Resolve templates in URL, Body, and Headers.
	url := m.resolveTemplate(step.URL, prev, args)
	body := m.resolveTemplate(step.Body, prev, args)
	headers := make(map[string]string, len(step.Headers))
	for k, v := range step.Headers {
		headers[k] = m.resolveTemplate(v, prev, args)
	}

	if url == "" {
		return nil, domain.NewSubSystemError("workflow", "Manager.executeHTTPStep", domain.ErrInvalidInput, "url is required")
	}

	if err := security.ValidateURL(url); err != nil {
		return nil, err
	}

	method := strings.ToUpper(step.Method)
	if method == "" {
		method = "GET"
	}
	allowed := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true}
	if !allowed[method] {
		return nil, domain.NewSubSystemError("workflow", "Manager.executeHTTPStep", domain.ErrInvalidInput,
			fmt.Sprintf("method %q not allowed", method))
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   toJSON(err.Error()),
			Error:    err.Error(),
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeHTTPStep", domain.ErrToolFailure, err.Error())
	}
	defer resp.Body.Close()

	maxRead := int64(maxOutput)
	if maxRead <= 0 {
		maxRead = int64(m.cfg.MaxOutput)
	}
	if maxRead <= 0 {
		maxRead = 1024 * 1024
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxRead))

	// Check fail_on_error.
	if step.FailOnError && resp.StatusCode >= 400 {
		errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)
		result := map[string]any{
			"status":      resp.StatusCode,
			"status_text": resp.Status,
			"body":        string(respBody),
		}
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   mustMarshal(result),
			Error:    errMsg,
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeHTTPStep", domain.ErrToolFailure, errMsg)
	}

	result := map[string]any{
		"status":      resp.StatusCode,
		"status_text": resp.Status,
		"body":        string(respBody),
	}

	return &domain.StepResult{
		StepID:   step.ID,
		Status:   "completed",
		Output:   mustMarshal(result),
		Duration: time.Since(start),
	}, nil
}

func (m *Manager) executeTransformStep(step domain.Step, prev []domain.StepResult, args map[string]string, maxOutput int, start time.Time) (*domain.StepResult, error) {
	if step.Template == "" {
		return nil, domain.NewSubSystemError("workflow", "Manager.executeTransformStep", domain.ErrInvalidInput, "template is required")
	}

	tmpl, err := template.New("transform").Parse(step.Template)
	if err != nil {
		return nil, domain.NewSubSystemError("workflow", "Manager.executeTransformStep", domain.ErrInvalidInput,
			fmt.Sprintf("invalid template: %v", err))
	}

	data := buildTemplateData(prev, args)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   toJSON(err.Error()),
			Error:    err.Error(),
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeTransformStep", domain.ErrToolFailure, err.Error())
	}

	output := m.truncateOutput(buf.String(), maxOutput)
	return &domain.StepResult{
		StepID:   step.ID,
		Status:   "completed",
		Output:   toJSON(output),
		Duration: time.Since(start),
	}, nil
}

func (m *Manager) executeApprovalStep(run *domain.WorkflowRun, step domain.Step, prev []domain.StepResult, args map[string]string, start time.Time) (*domain.StepResult, error) {
	// Resolve template in approval message.
	message := m.resolveTemplate(step.Message, prev, args)

	token := generateWorkflowID(time.Now())
	run.Status = "paused"
	run.ApprovalMessage = message
	run.ResumeToken = token
	run.UpdatedAt = time.Now()

	return &domain.StepResult{
		StepID:   step.ID,
		Status:   "completed",
		Output:   mustMarshal(map[string]string{"resume_token": token, "message": message}),
		Duration: time.Since(start),
	}, nil
}

func (m *Manager) executeToolCallStep(ctx context.Context, step domain.Step, prev []domain.StepResult, args map[string]string, maxOutput int, start time.Time) (*domain.StepResult, error) {
	if m.toolExec == nil {
		return nil, domain.NewDomainError("Manager.executeToolCallStep",
			domain.ErrToolFailure, "tool executor not configured")
	}
	if step.ToolName == "" {
		return nil, domain.NewSubSystemError("workflow", "Manager.executeToolCallStep",
			domain.ErrInvalidInput, "tool_name is required for tool_call step")
	}
	if blockedTools[step.ToolName] {
		return nil, domain.NewDomainError("Manager.executeToolCallStep",
			domain.ErrPermissionDenied, fmt.Sprintf("tool %q is blocked to prevent recursion", step.ToolName))
	}

	tool, err := m.toolExec.Get(step.ToolName)
	if err != nil {
		return nil, domain.NewDomainError("Manager.executeToolCallStep",
			domain.ErrToolFailure, fmt.Sprintf("tool %q not found: %v", step.ToolName, err))
	}

	// Resolve templates in tool params.
	params := json.RawMessage(step.ToolParams)
	if len(params) > 0 {
		resolved := m.resolveTemplate(string(params), prev, args)
		params = json.RawMessage(resolved)
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   toJSON(err.Error()),
			Error:    err.Error(),
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeToolCallStep", domain.ErrToolFailure, err.Error())
	}

	output := m.truncateOutput(result.Content, maxOutput)

	if result.IsError {
		return &domain.StepResult{
			StepID:   step.ID,
			Status:   "failed",
			Output:   toJSON(output),
			Error:    output,
			Duration: time.Since(start),
		}, domain.NewDomainError("Manager.executeToolCallStep", domain.ErrToolFailure, output)
	}

	return &domain.StepResult{
		StepID:   step.ID,
		Status:   "completed",
		Output:   toJSON(output),
		Duration: time.Since(start),
	}, nil
}

// --- helpers ---

func (m *Manager) validateCommand(command string) error {
	if command == "" {
		return domain.NewSubSystemError("workflow", "Manager.validateCommand", domain.ErrInvalidInput, "command is required")
	}
	allowed := m.cfg.WorkflowAllowedCommands
	if len(allowed) == 0 {
		allowed = m.cfg.AllowedCommands
	}
	base := filepath.Base(command)
	for _, a := range allowed {
		if base == a {
			return nil
		}
	}
	return domain.NewDomainError("Manager.validateCommand", domain.ErrCommandNotAllowed,
		fmt.Sprintf("command %q (base: %q) not in allowlist", command, base))
}

func (m *Manager) evaluateCondition(condition string, prev []domain.StepResult, args map[string]string) (bool, error) {
	tmpl, err := template.New("cond").Parse(condition)
	if err != nil {
		return false, fmt.Errorf("parse condition: %w", err)
	}
	data := buildTemplateData(prev, args)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return false, fmt.Errorf("evaluate condition: %w", err)
	}
	result := strings.TrimSpace(buf.String())
	return result != "" && result != "false" && result != "0" && result != "<no value>", nil
}

func (m *Manager) resolveTemplate(input string, prev []domain.StepResult, args map[string]string) string {
	if !strings.Contains(input, "{{") {
		return input
	}
	tmpl, err := template.New("arg").Parse(input)
	if err != nil {
		m.logger.Warn("template parse error, using raw input", "error", err)
		return input
	}
	data := buildTemplateData(prev, args)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		m.logger.Warn("template execution error, using raw input", "error", err)
		return input
	}
	return buf.String()
}

// truncateOutput safely truncates output to maxOutput bytes.
// If maxOutput <= 0, uses the configured MaxOutput.
// For valid JSON, produces a structured truncation envelope instead of cutting raw data.
func (m *Manager) truncateOutput(output string, maxOutput int) string {
	if maxOutput <= 0 {
		maxOutput = m.cfg.MaxOutput
	}
	if maxOutput <= 0 || len(output) <= maxOutput {
		return output
	}

	if json.Valid([]byte(output)) {
		// Scale preview to fit within maxOutput (leave room for envelope overhead).
		maxPreview := 200
		if maxOutput > 0 && maxOutput < 300 {
			maxPreview = maxOutput / 3
		}
		preview := output
		if len(preview) > maxPreview {
			preview = preview[:maxPreview]
		}
		envelope, _ := json.Marshal(map[string]any{
			"_truncated": true,
			"_preview":   preview,
			"_bytes":     len(output),
		})
		return string(envelope)
	}
	return output[:maxOutput] + "\n... (truncated)"
}

func (m *Manager) emitEvent(ctx context.Context, eventType domain.EventType, payload any) {
	if m.bus == nil {
		return
	}
	data, _ := json.Marshal(payload)
	m.bus.Publish(ctx, domain.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   data,
	})
}

func buildTemplateData(steps []domain.StepResult, args map[string]string) map[string]any {
	data := make(map[string]any, len(steps)+1)
	for _, s := range steps {
		entry := map[string]any{
			"status": s.Status,
			"error":  s.Error,
		}
		// Try to unmarshal output as a Go value.
		var v any
		if json.Unmarshal(s.Output, &v) == nil {
			entry["output"] = v
		} else {
			entry["output"] = string(s.Output)
		}
		data[s.StepID] = entry
	}
	if len(args) > 0 {
		data["args"] = args
	}
	return data
}

func validatePipeline(p domain.Pipeline) error {
	if len(p.Steps) == 0 {
		return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput, "pipeline has no steps")
	}
	seen := make(map[string]bool, len(p.Steps))
	validTypes := map[string]bool{
		"exec": true, "http": true, "transform": true, "approval": true, "tool_call": true,
	}
	for i, s := range p.Steps {
		if s.ID == "" {
			return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
				fmt.Sprintf("step[%d] has no id", i))
		}
		if seen[s.ID] {
			return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
				fmt.Sprintf("duplicate step id %q", s.ID))
		}
		seen[s.ID] = true

		if !validTypes[s.Type] {
			return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
				fmt.Sprintf("step %q has invalid type %q", s.ID, s.Type))
		}

		// Semantic validation per step type.
		switch s.Type {
		case "exec":
			if s.Command == "" {
				return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
					fmt.Sprintf("step %q (exec) requires command", s.ID))
			}
		case "http":
			if s.URL == "" {
				return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
					fmt.Sprintf("step %q (http) requires url", s.ID))
			}
		case "transform":
			if s.Template == "" {
				return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
					fmt.Sprintf("step %q (transform) requires template", s.ID))
			}
		case "tool_call":
			if s.ToolName == "" {
				return domain.NewSubSystemError("workflow", "validatePipeline", domain.ErrInvalidInput,
					fmt.Sprintf("step %q (tool_call) requires tool_name", s.ID))
			}
		}
	}
	return nil
}

func validatePipelineArgs(p domain.Pipeline, env map[string]string) error {
	for name, arg := range p.Args {
		if arg.Required {
			if _, ok := env[name]; !ok {
				return domain.NewSubSystemError("workflow", "validatePipelineArgs",
					domain.ErrInvalidInput, fmt.Sprintf("required arg %q not provided", name))
			}
		}
	}
	return nil
}

func generateWorkflowID(t time.Time) string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}

func toJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return data
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
