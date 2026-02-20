package tool

import (
	"context"
	"encoding/json"
	"testing"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/cronjob"
	"alfred-ai/internal/usecase/scheduling"
)

func newTestCronTool(t *testing.T) *CronTool {
	t.Helper()
	dir := t.TempDir()
	store, err := cronjob.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	sched := scheduling.NewScheduler(newTestLogger())
	sched.Start(context.Background())
	t.Cleanup(func() { sched.Stop() })

	mgr := cronjob.NewManager(store, sched, nil, newTestLogger())
	return NewCronTool(mgr, newTestLogger())
}

func execCronTool(t *testing.T, tool *CronTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

func TestCronToolCreate(t *testing.T) {
	ct := newTestCronTool(t)

	result := execCronTool(t, ct, map[string]any{
		"action":  "create",
		"name":    "test-job",
		"message": "remind me",
		"schedule": map[string]any{
			"kind":     "every",
			"every_ms": 60000,
		},
	})

	if result.IsError {
		t.Fatalf("create failed: %s", result.Content)
	}

	var job domain.CronJob
	if err := json.Unmarshal([]byte(result.Content), &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.ID == "" {
		t.Error("expected non-empty job ID")
	}
	if job.Name != "test-job" {
		t.Errorf("got name %q, want %q", job.Name, "test-job")
	}
}

func TestCronToolCreateMissingSchedule(t *testing.T) {
	ct := newTestCronTool(t)

	result := execCronTool(t, ct, map[string]any{
		"action":  "create",
		"message": "hi",
	})
	if !result.IsError {
		t.Error("expected error for missing schedule")
	}
}

func TestCronToolCreateMissingMessage(t *testing.T) {
	ct := newTestCronTool(t)

	result := execCronTool(t, ct, map[string]any{
		"action": "create",
		"schedule": map[string]any{
			"kind":     "every",
			"every_ms": 60000,
		},
	})
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

func TestCronToolList(t *testing.T) {
	ct := newTestCronTool(t)

	// Create two jobs.
	execCronTool(t, ct, map[string]any{
		"action": "create", "name": "j1", "message": "hi",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})
	execCronTool(t, ct, map[string]any{
		"action": "create", "name": "j2", "message": "hi",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})

	result := execCronTool(t, ct, map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("list failed: %s", result.Content)
	}

	var jobs []domain.CronJob
	json.Unmarshal([]byte(result.Content), &jobs)
	if len(jobs) != 2 {
		t.Errorf("got %d jobs, want 2", len(jobs))
	}
}

func TestCronToolGetAndDelete(t *testing.T) {
	ct := newTestCronTool(t)

	// Create a job.
	createResult := execCronTool(t, ct, map[string]any{
		"action": "create", "name": "deletable", "message": "hi",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})
	var job domain.CronJob
	json.Unmarshal([]byte(createResult.Content), &job)

	// Get it.
	getResult := execCronTool(t, ct, map[string]any{"action": "get", "job_id": job.ID})
	if getResult.IsError {
		t.Fatalf("get failed: %s", getResult.Content)
	}

	// Delete it.
	delResult := execCronTool(t, ct, map[string]any{"action": "delete", "job_id": job.ID})
	if delResult.IsError {
		t.Fatalf("delete failed: %s", delResult.Content)
	}

	// Get should fail now.
	getResult2 := execCronTool(t, ct, map[string]any{"action": "get", "job_id": job.ID})
	if !getResult2.IsError {
		t.Error("expected error after delete")
	}
}

func TestCronToolUpdate(t *testing.T) {
	ct := newTestCronTool(t)

	createResult := execCronTool(t, ct, map[string]any{
		"action": "create", "name": "updatable", "message": "old",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})
	var job domain.CronJob
	json.Unmarshal([]byte(createResult.Content), &job)

	updateResult := execCronTool(t, ct, map[string]any{
		"action":  "update",
		"job_id":  job.ID,
		"name":    "new-name",
		"message": "new-msg",
	})
	if updateResult.IsError {
		t.Fatalf("update failed: %s", updateResult.Content)
	}

	var updated domain.CronJob
	json.Unmarshal([]byte(updateResult.Content), &updated)
	if updated.Name != "new-name" {
		t.Errorf("got name %q, want %q", updated.Name, "new-name")
	}
}

func TestCronToolRuns(t *testing.T) {
	ct := newTestCronTool(t)

	createResult := execCronTool(t, ct, map[string]any{
		"action": "create", "name": "with-runs", "message": "hi",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})
	var job domain.CronJob
	json.Unmarshal([]byte(createResult.Content), &job)

	// No runs yet.
	runsResult := execCronTool(t, ct, map[string]any{"action": "runs", "job_id": job.ID})
	if runsResult.IsError {
		t.Fatalf("runs failed: %s", runsResult.Content)
	}
}

func TestCronToolUnknownAction(t *testing.T) {
	ct := newTestCronTool(t)
	result := execCronTool(t, ct, map[string]any{"action": "bad"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestCronToolMissingJobID(t *testing.T) {
	ct := newTestCronTool(t)

	for _, action := range []string{"get", "update", "delete", "runs"} {
		result := execCronTool(t, ct, map[string]any{"action": action})
		if !result.IsError {
			t.Errorf("expected error for %s without job_id", action)
		}
	}
}

func TestCronToolInvalidParams(t *testing.T) {
	ct := newTestCronTool(t)
	result, err := ct.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}
