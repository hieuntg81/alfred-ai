package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newTestRun(id, status string) domain.WorkflowRun {
	return domain.WorkflowRun{
		ID:           id,
		PipelineName: "test-pipeline",
		Status:       status,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Pipeline: domain.Pipeline{
			Name:  "test-pipeline",
			Steps: []domain.Step{{ID: "s1", Type: "exec", Command: "echo"}},
		},
		Steps: []domain.StepResult{
			{StepID: "s1", Status: "completed", Output: json.RawMessage(`"ok"`)},
		},
	}
}

func TestFileStoreSaveAndGet(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()
	run := newTestRun("run-1", "completed")

	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := store.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ID != "run-1" || got.PipelineName != "test-pipeline" {
		t.Errorf("unexpected run: %+v", got)
	}
}

func TestFileStoreList(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()

	r1 := newTestRun("run-1", "completed")
	r1.CreatedAt = time.Now().Add(-2 * time.Hour)
	r2 := newTestRun("run-2", "completed")
	r2.CreatedAt = time.Now().Add(-1 * time.Hour)
	r3 := newTestRun("run-3", "running")
	r3.CreatedAt = time.Now()

	store.SaveRun(ctx, r1)
	store.SaveRun(ctx, r2)
	store.SaveRun(ctx, r3)

	runs, err := store.ListRuns(ctx, 2)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	// Newest first.
	if runs[0].ID != "run-3" {
		t.Errorf("expected run-3 first, got %s", runs[0].ID)
	}
}

func TestFileStoreDelete(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()
	store.SaveRun(ctx, newTestRun("run-1", "completed"))

	if err := store.DeleteRun(ctx, "run-1"); err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}

	_, err = store.GetRun(ctx, "run-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestFileStoreNotFound(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	_, err = store.GetRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent run")
	}
}

func TestFileStoreGetByToken(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()
	run := newTestRun("run-1", "paused")
	run.ResumeToken = "tok-abc"
	store.SaveRun(ctx, run)

	got, err := store.GetRunByToken(ctx, "tok-abc")
	if err != nil {
		t.Fatalf("GetRunByToken: %v", err)
	}
	if got.ID != "run-1" {
		t.Errorf("expected run-1, got %s", got.ID)
	}
}

func TestFileStoreGetByTokenNotFound(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	_, err = store.GetRunByToken(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestFileStorePersistence(t *testing.T) {
	dir := t.TempDir()

	store1, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()
	store1.SaveRun(ctx, newTestRun("run-1", "completed"))

	// Create a new store from the same directory.
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore (reload): %v", err)
	}

	got, err := store2.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun after reload: %v", err)
	}
	if got.ID != "run-1" {
		t.Errorf("expected run-1 after reload, got %s", got.ID)
	}
}

func TestFileStoreDeleteNotFound(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	err = store.DeleteRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for delete of nonexistent run")
	}
}
