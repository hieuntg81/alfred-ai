package cronjob

import (
	"context"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newTestJob(id, name string) domain.CronJob {
	return domain.CronJob{
		ID:        id,
		Name:      name,
		Schedule:  domain.CronSchedule{Kind: "cron", Expression: "*/5 * * * *"},
		Action:    domain.CronAction{Kind: "agent_run", Message: "hello"},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestFileStoreSaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	ctx := context.Background()
	job := newTestJob("j1", "test-job")

	if err := store.Save(ctx, job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(ctx, "j1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "test-job" {
		t.Errorf("got name %q, want %q", got.Name, "test-job")
	}
}

func TestFileStoreGetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	_, err := store.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestFileStoreList(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	ctx := context.Background()

	j1 := newTestJob("j1", "first")
	j1.CreatedAt = time.Now().Add(-time.Hour)
	j2 := newTestJob("j2", "second")
	j2.CreatedAt = time.Now()

	store.Save(ctx, j1)
	store.Save(ctx, j2)

	jobs, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	if jobs[0].ID != "j1" {
		t.Error("expected jobs sorted by created_at")
	}
}

func TestFileStoreDelete(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	ctx := context.Background()

	store.Save(ctx, newTestJob("j1", "deletable"))

	if err := store.Delete(ctx, "j1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Get(ctx, "j1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestFileStoreDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)

	err := store.Delete(context.Background(), "nope")
	if err == nil {
		t.Error("expected error for nonexistent delete")
	}
}

func TestFileStoreRuns(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		store.SaveRun(ctx, domain.CronRun{
			JobID:     "j1",
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
			Duration:  "100ms",
			Success:   true,
		})
	}

	runs, err := store.ListRuns(ctx, "j1", 3)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	// Most recent first.
	if runs[0].StartedAt.Before(runs[1].StartedAt) {
		t.Error("runs should be most recent first")
	}
}

func TestFileStoreRunsLimit(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	ctx := context.Background()

	// Add more than maxRunsPerJob runs.
	for i := 0; i < maxRunsPerJob+20; i++ {
		store.SaveRun(ctx, domain.CronRun{
			JobID:     "j1",
			StartedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
			Success:   true,
		})
	}

	runs, err := store.ListRuns(ctx, "j1", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != maxRunsPerJob {
		t.Errorf("got %d runs, want %d (capped)", len(runs), maxRunsPerJob)
	}
}

func TestFileStorePersistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Write data.
	store1, _ := NewFileStore(dir)
	store1.Save(ctx, newTestJob("j1", "persistent"))
	store1.SaveRun(ctx, domain.CronRun{JobID: "j1", StartedAt: time.Now(), Success: true})

	// Re-open and verify.
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}

	got, err := store2.Get(ctx, "j1")
	if err != nil {
		t.Fatalf("Get after re-open: %v", err)
	}
	if got.Name != "persistent" {
		t.Errorf("got name %q, want %q", got.Name, "persistent")
	}

	runs, _ := store2.ListRuns(ctx, "j1", 10)
	if len(runs) != 1 {
		t.Errorf("got %d runs after re-open, want 1", len(runs))
	}
}

func TestFileStoreDeleteCleansRuns(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewFileStore(dir)
	ctx := context.Background()

	store.Save(ctx, newTestJob("j1", "with-runs"))
	store.SaveRun(ctx, domain.CronRun{JobID: "j1", StartedAt: time.Now(), Success: true})
	store.Delete(ctx, "j1")

	runs, _ := store.ListRuns(ctx, "j1", 10)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after delete, got %d", len(runs))
	}
}
