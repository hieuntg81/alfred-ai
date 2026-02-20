package cronjob

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/scheduling"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestManager(t *testing.T) (*Manager, *FileStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	sched := scheduling.NewScheduler(newTestLogger())
	sched.Start(context.Background())
	t.Cleanup(func() { sched.Stop() })

	mgr := NewManager(store, sched, nil, newTestLogger())
	return mgr, store
}

func TestManagerCreate(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	job, err := mgr.Create(ctx, domain.CronJob{
		Name:     "test-create",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hello"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.ID == "" {
		t.Error("expected non-empty ID")
	}
	if !job.Enabled {
		t.Error("expected job to be enabled")
	}
}

func TestManagerCreateInvalidSchedule(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Create(ctx, domain.CronJob{
		Name:     "bad-schedule",
		Schedule: domain.CronSchedule{Kind: "unknown"},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hello"},
	})
	if err == nil {
		t.Error("expected error for invalid schedule kind")
	}
}

func TestManagerCreateMissingMessage(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Create(ctx, domain.CronJob{
		Name:     "no-msg",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run"},
	})
	if err == nil {
		t.Error("expected error for missing message")
	}
}

func TestManagerListAndGet(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	mgr.Create(ctx, domain.CronJob{
		Name:     "job-1",
		Schedule: domain.CronSchedule{Kind: "cron", Expression: "*/5 * * * *"},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})
	mgr.Create(ctx, domain.CronJob{
		Name:     "job-2",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 30000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})

	jobs, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}

	got, err := mgr.Get(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != jobs[0].Name {
		t.Errorf("got name %q, want %q", got.Name, jobs[0].Name)
	}
}

func TestManagerUpdate(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	job, _ := mgr.Create(ctx, domain.CronJob{
		Name:     "updatable",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hello"},
	})

	newName := "updated-name"
	newMsg := "updated-msg"
	updated, err := mgr.Update(ctx, job.ID, Patch{
		Name:    &newName,
		Message: &newMsg,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "updated-name" {
		t.Errorf("got name %q, want %q", updated.Name, "updated-name")
	}
	if updated.Action.Message != "updated-msg" {
		t.Errorf("got message %q, want %q", updated.Action.Message, "updated-msg")
	}
}

func TestManagerUpdateSchedule(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	job, _ := mgr.Create(ctx, domain.CronJob{
		Name:     "reschedule",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})

	newSched := &domain.CronSchedule{Kind: "cron", Expression: "*/10 * * * *"}
	_, err := mgr.Update(ctx, job.ID, Patch{Schedule: newSched})
	if err != nil {
		t.Fatalf("Update schedule: %v", err)
	}
}

func TestManagerDisableEnable(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	job, _ := mgr.Create(ctx, domain.CronJob{
		Name:     "toggle",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})

	disabled := false
	_, err := mgr.Update(ctx, job.ID, Patch{Enabled: &disabled})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}

	got, _ := mgr.Get(ctx, job.ID)
	if got.Enabled {
		t.Error("expected job to be disabled")
	}

	enabled := true
	_, err = mgr.Update(ctx, job.ID, Patch{Enabled: &enabled})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}

	got, _ = mgr.Get(ctx, job.ID)
	if !got.Enabled {
		t.Error("expected job to be re-enabled")
	}
}

func TestManagerDelete(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	job, _ := mgr.Create(ctx, domain.CronJob{
		Name:     "deletable",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})

	if err := mgr.Delete(ctx, job.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := mgr.Get(ctx, job.ID)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestManagerLoadAndSchedule(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Create a job.
	store1, _ := NewFileStore(dir)
	sched1 := scheduling.NewScheduler(newTestLogger())
	sched1.Start(ctx)
	mgr1 := NewManager(store1, sched1, nil, newTestLogger())

	job, _ := mgr1.Create(ctx, domain.CronJob{
		Name:     "persistent",
		Schedule: domain.CronSchedule{Kind: "every", EveryMs: 60000},
		Action:   domain.CronAction{Kind: "agent_run", Message: "hi"},
	})
	sched1.Stop()

	// Phase 2: Reload from disk.
	store2, _ := NewFileStore(dir)
	sched2 := scheduling.NewScheduler(newTestLogger())
	sched2.Start(ctx)
	t.Cleanup(func() { sched2.Stop() })
	mgr2 := NewManager(store2, sched2, nil, newTestLogger())

	if err := mgr2.LoadAndSchedule(ctx); err != nil {
		t.Fatalf("LoadAndSchedule: %v", err)
	}

	// Verify the job was restored and scheduled.
	got, err := mgr2.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.Name != "persistent" {
		t.Errorf("got name %q, want %q", got.Name, "persistent")
	}
	if got.NextRunAt == nil {
		t.Error("expected NextRunAt to be set after scheduling")
	}
}

func TestValidateCronSchedule(t *testing.T) {
	tests := []struct {
		name    string
		sched   domain.CronSchedule
		wantErr bool
	}{
		{"valid cron", domain.CronSchedule{Kind: "cron", Expression: "*/5 * * * *"}, false},
		{"valid every", domain.CronSchedule{Kind: "every", EveryMs: 60000}, false},
		{"valid at", domain.CronSchedule{Kind: "at", At: time.Now().Add(time.Hour).Format(time.RFC3339)}, false},
		{"unknown kind", domain.CronSchedule{Kind: "bad"}, true},
		{"cron missing expression", domain.CronSchedule{Kind: "cron"}, true},
		{"every zero ms", domain.CronSchedule{Kind: "every", EveryMs: 0}, true},
		{"at missing timestamp", domain.CronSchedule{Kind: "at"}, true},
		{"at invalid timestamp", domain.CronSchedule{Kind: "at", At: "not-a-date"}, true},
		{"invalid cron expression", domain.CronSchedule{Kind: "cron", Expression: "bad"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCronSchedule(tt.sched)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCronSchedule(%+v) error = %v, wantErr %v", tt.sched, err, tt.wantErr)
			}
		})
	}
}
