package scheduling

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduledAction identifies a type of scheduled action.
type ScheduledAction string

const (
	ActionMemorySync      ScheduledAction = "memory_sync"
	ActionMemoryCurate    ScheduledAction = "memory_curate"
	ActionSessionReap     ScheduledAction = "session_reap"
	ActionAgentRun        ScheduledAction = "agent_run"
	ActionAuditRetention  ScheduledAction = "audit_retention"
)

// ScheduledTask defines a recurring task.
type ScheduledTask struct {
	Name     string
	Schedule string // cron expression "*/5 * * * *" OR duration "30m"
	Action   ScheduledAction
	AgentID  string // for agent_run
	Channel  string // for agent_run
	Message  string // for agent_run
	OneShot  bool
}

// Scheduler runs tasks on a recurring schedule using cron expressions or durations.
type Scheduler struct {
	cron           *cron.Cron
	actions        map[ScheduledAction]func(ctx context.Context) error
	dynamicEntries map[string]cron.EntryID // id → entryID for runtime-added tasks
	logger         *slog.Logger
	mu             sync.Mutex
	started        bool
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewScheduler creates a scheduler.
func NewScheduler(logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:           cron.New(),
		actions:        make(map[ScheduledAction]func(ctx context.Context) error),
		dynamicEntries: make(map[string]cron.EntryID),
		logger:         logger,
	}
}

// RegisterAction registers a handler for a scheduled action type.
func (s *Scheduler) RegisterAction(action ScheduledAction, fn func(ctx context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions[action] = fn
}

// AddTask adds a scheduled task. The schedule can be a cron expression or a duration string.
func (s *Scheduler) AddTask(task ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn, ok := s.actions[task.Action]
	if !ok {
		return fmt.Errorf("scheduler: unknown action %q for task %q", task.Action, task.Name)
	}

	schedule, err := parseSchedule(task.Schedule)
	if err != nil {
		return fmt.Errorf("scheduler: invalid schedule %q for task %q: %w", task.Schedule, task.Name, err)
	}

	taskName := task.Name
	oneShot := task.OneShot
	logger := s.logger

	var entryID cron.EntryID
	entryID = s.cron.Schedule(schedule, cron.FuncJob(func() {
		// Read context under lock
		s.mu.Lock()
		ctx := s.ctx
		s.mu.Unlock()

		if ctx == nil {
			logger.Debug("scheduler stopped, skipping task", "task", taskName)
			return
		}

		// Add timeout for individual task
		taskCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		start := time.Now()
		if err := fn(taskCtx); err != nil {
			logger.Warn("scheduled task failed",
				"task", taskName,
				"error", err,
				"duration", time.Since(start))
		} else {
			logger.Info("scheduled task completed",
				"task", taskName,
				"duration", time.Since(start))
		}

		if oneShot {
			s.cron.Remove(entryID)
		}
	}))

	logger.Info("task added to scheduler", "name", task.Name, "schedule", task.Schedule, "action", string(task.Action))
	return nil
}

// Start begins running the scheduler.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.cron.Start()
	s.started = true
	return nil
}

// Stop signals the scheduler to stop and waits for running jobs to finish.
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	stopCtx := s.cron.Stop()
	<-stopCtx.Done()
	s.started = false
	return nil
}

// parseSchedule tries to parse a schedule string as a cron expression first,
// then falls back to time.ParseDuration.
func parseSchedule(schedule string) (cron.Schedule, error) {
	if schedule == "" {
		return nil, fmt.Errorf("empty schedule")
	}

	// Try cron expression first.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if sched, err := parser.Parse(schedule); err == nil {
		return sched, nil
	}

	// Fall back to duration.
	dur, err := time.ParseDuration(schedule)
	if err != nil {
		return nil, fmt.Errorf("not a valid cron expression or duration: %q", schedule)
	}
	if dur <= 0 {
		return nil, fmt.Errorf("duration must be positive: %q", schedule)
	}
	return &constantDelay{delay: dur}, nil
}

// AddDynamicTask adds a runtime task identified by id.
// The caller provides a pre-parsed cron.Schedule and the function to run.
func (s *Scheduler) AddDynamicTask(id string, schedule cron.Schedule, fn func(ctx context.Context) error, oneShot bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.dynamicEntries[id]; exists {
		return fmt.Errorf("scheduler: dynamic task %q already exists", id)
	}

	logger := s.logger
	var entryID cron.EntryID
	entryID = s.cron.Schedule(schedule, cron.FuncJob(func() {
		s.mu.Lock()
		ctx := s.ctx
		s.mu.Unlock()

		if ctx == nil {
			logger.Debug("scheduler stopped, skipping dynamic task", "id", id)
			return
		}

		taskCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		start := time.Now()
		if err := fn(taskCtx); err != nil {
			logger.Warn("dynamic task failed", "id", id, "error", err, "duration", time.Since(start))
		} else {
			logger.Info("dynamic task completed", "id", id, "duration", time.Since(start))
		}

		if oneShot {
			s.cron.Remove(entryID)
			s.mu.Lock()
			delete(s.dynamicEntries, id)
			s.mu.Unlock()
		}
	}))

	s.dynamicEntries[id] = entryID
	logger.Info("dynamic task added", "id", id)
	return nil
}

// RemoveDynamicTask removes a runtime task by id.
func (s *Scheduler) RemoveDynamicTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, ok := s.dynamicEntries[id]
	if !ok {
		return fmt.Errorf("scheduler: dynamic task %q not found", id)
	}
	s.cron.Remove(entryID)
	delete(s.dynamicEntries, id)
	s.logger.Info("dynamic task removed", "id", id)
	return nil
}

// GetNextRun returns the next scheduled run time for a dynamic task, or nil if not found.
func (s *Scheduler) GetNextRun(id string) *time.Time {
	s.mu.Lock()
	entryID, ok := s.dynamicEntries[id]
	s.mu.Unlock()

	if !ok {
		return nil
	}
	entry := s.cron.Entry(entryID)
	if entry.ID == 0 {
		return nil
	}
	t := entry.Next
	return &t
}

// ParseSchedule exposes schedule parsing for external callers.
func ParseSchedule(schedule string) (cron.Schedule, error) {
	return parseSchedule(schedule)
}

// NewConstantDelay returns a cron.Schedule that fires at a fixed interval.
// Useful for callers that need to build schedules programmatically.
func NewConstantDelay(d time.Duration) cron.Schedule {
	return &constantDelay{delay: d}
}

// constantDelay implements cron.Schedule for a fixed interval.
// Unlike cron.Every(), it supports sub-second durations.
type constantDelay struct {
	delay time.Duration
}

func (d *constantDelay) Next(t time.Time) time.Time {
	return t.Add(d.delay)
}
