package scheduling

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSchedulerStartStop(t *testing.T) {
	s := NewScheduler(newTestLogger())

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSchedulerActionFires(t *testing.T) {
	var count atomic.Int32

	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error {
		count.Add(1)
		return nil
	})
	if err := s.AddTask(ScheduledTask{
		Name: "test-task", Schedule: "50ms", Action: ActionMemorySync,
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if c := count.Load(); c < 1 {
		t.Errorf("action fired %d times, expected at least 1", c)
	}
}

func TestSchedulerUnknownAction(t *testing.T) {
	s := NewScheduler(newTestLogger())

	err := s.AddTask(ScheduledTask{
		Name: "unknown", Schedule: "100ms", Action: "does_not_exist",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestSchedulerContextCancellation(t *testing.T) {
	var count atomic.Int32

	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error {
		count.Add(1)
		return nil
	})
	s.AddTask(ScheduledTask{
		Name: "ctx-task", Schedule: "50ms", Action: ActionMemorySync,
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()
	s.Stop()

	countAfterCancel := count.Load()
	time.Sleep(100 * time.Millisecond)

	if count.Load() != countAfterCancel {
		t.Error("task continued after context cancellation")
	}
}

func TestSchedulerMultipleTasks(t *testing.T) {
	var syncCount, curateCount atomic.Int32

	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error {
		syncCount.Add(1)
		return nil
	})
	s.RegisterAction(ActionMemoryCurate, func(ctx context.Context) error {
		curateCount.Add(1)
		return nil
	})

	s.AddTask(ScheduledTask{Name: "sync", Schedule: "50ms", Action: ActionMemorySync})
	s.AddTask(ScheduledTask{Name: "curate", Schedule: "50ms", Action: ActionMemoryCurate})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if syncCount.Load() < 1 {
		t.Error("sync action never fired")
	}
	if curateCount.Load() < 1 {
		t.Error("curate action never fired")
	}
}

func TestSchedulerActionError(t *testing.T) {
	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error {
		return fmt.Errorf("simulated error")
	})
	s.AddTask(ScheduledTask{Name: "failing", Schedule: "50ms", Action: ActionMemorySync})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(150 * time.Millisecond)

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSchedulerDoubleStop(t *testing.T) {
	s := NewScheduler(newTestLogger())
	s.Start(context.Background())

	if err := s.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestSchedulerStopWithoutStart(t *testing.T) {
	s := NewScheduler(newTestLogger())
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop without start: %v", err)
	}
}

func TestParseScheduleCron(t *testing.T) {
	sched, err := parseSchedule("*/5 * * * *")
	if err != nil {
		t.Fatalf("parseSchedule cron: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil schedule")
	}
}

func TestParseScheduleCronDescriptor(t *testing.T) {
	sched, err := parseSchedule("@every 30m")
	if err != nil {
		t.Fatalf("parseSchedule @every: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil schedule")
	}
}

func TestParseScheduleDuration(t *testing.T) {
	sched, err := parseSchedule("30m")
	if err != nil {
		t.Fatalf("parseSchedule duration: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil schedule")
	}
}

func TestParseScheduleSmallDuration(t *testing.T) {
	sched, err := parseSchedule("100ms")
	if err != nil {
		t.Fatalf("parseSchedule 100ms: %v", err)
	}
	if sched == nil {
		t.Fatal("expected non-nil schedule")
	}
}

func TestParseScheduleInvalid(t *testing.T) {
	_, err := parseSchedule("not-a-schedule")
	if err == nil {
		t.Error("expected error for invalid schedule")
	}
}

func TestParseScheduleEmpty(t *testing.T) {
	_, err := parseSchedule("")
	if err == nil {
		t.Error("expected error for empty schedule")
	}
}

func TestParseScheduleNegative(t *testing.T) {
	_, err := parseSchedule("-5m")
	if err == nil {
		t.Error("expected error for negative duration")
	}
}

func TestSchedulerNewActionTypes(t *testing.T) {
	s := NewScheduler(newTestLogger())

	var reapCalled, agentCalled atomic.Int32
	s.RegisterAction(ActionSessionReap, func(ctx context.Context) error {
		reapCalled.Add(1)
		return nil
	})
	s.RegisterAction(ActionAgentRun, func(ctx context.Context) error {
		agentCalled.Add(1)
		return nil
	})

	s.AddTask(ScheduledTask{Name: "reap", Schedule: "50ms", Action: ActionSessionReap})
	s.AddTask(ScheduledTask{Name: "agent", Schedule: "50ms", Action: ActionAgentRun, AgentID: "main", Message: "hello"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if reapCalled.Load() < 1 {
		t.Error("session_reap never fired")
	}
	if agentCalled.Load() < 1 {
		t.Error("agent_run never fired")
	}
}

func TestSchedulerOneShot(t *testing.T) {
	var count atomic.Int32

	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error {
		count.Add(1)
		return nil
	})
	if err := s.AddTask(ScheduledTask{
		Name: "one-shot", Schedule: "50ms", Action: ActionMemorySync, OneShot: true,
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	// Wait for first fire + extra cycles.
	time.Sleep(300 * time.Millisecond)
	s.Stop()

	if c := count.Load(); c != 1 {
		t.Errorf("one-shot fired %d times, expected exactly 1", c)
	}
}

func TestSchedulerInvalidSchedule(t *testing.T) {
	s := NewScheduler(newTestLogger())
	s.RegisterAction(ActionMemorySync, func(ctx context.Context) error { return nil })

	err := s.AddTask(ScheduledTask{Name: "bad", Schedule: "not-valid", Action: ActionMemorySync})
	if err == nil {
		t.Error("expected error for invalid schedule string")
	}
}

func TestSchedulerAddDynamicTask(t *testing.T) {
	var count atomic.Int32
	s := NewScheduler(newTestLogger())

	sched, _ := ParseSchedule("50ms")
	err := s.AddDynamicTask("job-1", sched, func(ctx context.Context) error {
		count.Add(1)
		return nil
	}, false)
	if err != nil {
		t.Fatalf("AddDynamicTask: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if c := count.Load(); c < 1 {
		t.Errorf("dynamic task fired %d times, expected at least 1", c)
	}
}

func TestSchedulerAddDynamicTaskDuplicate(t *testing.T) {
	s := NewScheduler(newTestLogger())

	sched, _ := ParseSchedule("1h")
	_ = s.AddDynamicTask("dup", sched, func(ctx context.Context) error { return nil }, false)
	err := s.AddDynamicTask("dup", sched, func(ctx context.Context) error { return nil }, false)
	if err == nil {
		t.Error("expected error for duplicate dynamic task id")
	}
}

func TestSchedulerRemoveDynamicTask(t *testing.T) {
	var count atomic.Int32
	s := NewScheduler(newTestLogger())

	sched, _ := ParseSchedule("50ms")
	_ = s.AddDynamicTask("removable", sched, func(ctx context.Context) error {
		count.Add(1)
		return nil
	}, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	if err := s.RemoveDynamicTask("removable"); err != nil {
		t.Fatalf("RemoveDynamicTask: %v", err)
	}

	countAfterRemove := count.Load()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	if count.Load() > countAfterRemove+1 {
		t.Error("task continued firing after removal")
	}
}

func TestSchedulerRemoveDynamicTaskNotFound(t *testing.T) {
	s := NewScheduler(newTestLogger())
	err := s.RemoveDynamicTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent dynamic task")
	}
}

func TestSchedulerGetNextRun(t *testing.T) {
	s := NewScheduler(newTestLogger())

	sched, _ := ParseSchedule("1h")
	_ = s.AddDynamicTask("next-run", sched, func(ctx context.Context) error { return nil }, false)

	s.Start(context.Background())
	defer s.Stop()

	next := s.GetNextRun("next-run")
	if next == nil {
		t.Fatal("expected non-nil next run time")
	}
	if next.Before(time.Now()) {
		t.Error("next run should be in the future")
	}
}

func TestSchedulerGetNextRunNotFound(t *testing.T) {
	s := NewScheduler(newTestLogger())
	if s.GetNextRun("nope") != nil {
		t.Error("expected nil for unknown task")
	}
}

func TestSchedulerDynamicOneShot(t *testing.T) {
	var count atomic.Int32
	s := NewScheduler(newTestLogger())

	sched, _ := ParseSchedule("50ms")
	_ = s.AddDynamicTask("one-shot-dyn", sched, func(ctx context.Context) error {
		count.Add(1)
		return nil
	}, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	time.Sleep(300 * time.Millisecond)
	s.Stop()

	if c := count.Load(); c != 1 {
		t.Errorf("one-shot dynamic task fired %d times, expected exactly 1", c)
	}
}
