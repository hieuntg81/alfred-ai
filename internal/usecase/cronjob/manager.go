package cronjob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/robfig/cron/v3"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/usecase/scheduling"
)

// MessageHandler abstracts message routing so that Manager does not depend on
// the concrete Router type (avoids import cycle with the parent usecase package).
type MessageHandler interface {
	Handle(ctx context.Context, msg domain.InboundMessage) (domain.OutboundMessage, error)
}

// Patch contains optional fields for updating a cron job.
type Patch struct {
	Name     *string              `json:"name,omitempty"`
	Schedule *domain.CronSchedule `json:"schedule,omitempty"`
	Message  *string              `json:"message,omitempty"`
	Enabled  *bool                `json:"enabled,omitempty"`
}

// Manager orchestrates cron job CRUD, scheduling, and execution.
type Manager struct {
	store     domain.CronStore
	scheduler *scheduling.Scheduler
	handler   MessageHandler
	bus       domain.EventBus
	logger    *slog.Logger
	mu        sync.Mutex
}

// NewManager creates a Manager. The handler can be set later via SetHandler.
func NewManager(store domain.CronStore, scheduler *scheduling.Scheduler, bus domain.EventBus, logger *slog.Logger) *Manager {
	return &Manager{
		store:     store,
		scheduler: scheduler,
		bus:       bus,
		logger:    logger,
	}
}

// SetHandler sets the message handler for executing agent_run actions.
// Must be called before LoadAndSchedule.
func (m *Manager) SetHandler(handler MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

// Create creates and schedules a new cron job.
func (m *Manager) Create(ctx context.Context, job domain.CronJob) (*domain.CronJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job.ID == "" {
		job.ID = m.newID()
	}
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	job.Enabled = true

	if err := validateCronSchedule(job.Schedule); err != nil {
		return nil, domain.WrapOp("cronmanager", err)
	}
	if job.Action.Message == "" {
		return nil, fmt.Errorf("cronmanager: action message is required")
	}

	if err := m.store.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("cronmanager: save: %w", err)
	}

	if err := m.scheduleJob(job); err != nil {
		// Best effort: remove from store if scheduling fails.
		m.store.Delete(ctx, job.ID)
		return nil, fmt.Errorf("cronmanager: schedule: %w", err)
	}

	m.emitEvent(ctx, domain.EventCronJobCreated, job)
	m.logger.Info("cron job created", "id", job.ID, "name", job.Name)

	next := m.scheduler.GetNextRun(job.ID)
	job.NextRunAt = next

	return &job, nil
}

// List returns all cron jobs.
func (m *Manager) List(ctx context.Context) ([]domain.CronJob, error) {
	jobs, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].NextRunAt = m.scheduler.GetNextRun(jobs[i].ID)
	}
	return jobs, nil
}

// Get returns a single cron job by ID.
func (m *Manager) Get(ctx context.Context, id string) (*domain.CronJob, error) {
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	job.NextRunAt = m.scheduler.GetNextRun(id)
	return job, nil
}

// Update patches a cron job and reschedules if the schedule changed.
func (m *Manager) Update(ctx context.Context, id string, patch Patch) (*domain.CronJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	scheduleChanged := false

	if patch.Name != nil {
		job.Name = *patch.Name
	}
	if patch.Message != nil {
		job.Action.Message = *patch.Message
	}
	if patch.Enabled != nil {
		if *patch.Enabled != job.Enabled {
			job.Enabled = *patch.Enabled
			scheduleChanged = true
		}
	}
	if patch.Schedule != nil {
		if err := validateCronSchedule(*patch.Schedule); err != nil {
			return nil, domain.WrapOp("cronmanager", err)
		}
		job.Schedule = *patch.Schedule
		scheduleChanged = true
	}

	job.UpdatedAt = time.Now()

	if err := m.store.Save(ctx, *job); err != nil {
		return nil, fmt.Errorf("cronmanager: save: %w", err)
	}

	if scheduleChanged {
		// Remove old schedule (ignore error if not found).
		m.scheduler.RemoveDynamicTask(id)

		if job.Enabled {
			if err := m.scheduleJob(*job); err != nil {
				return nil, fmt.Errorf("cronmanager: reschedule: %w", err)
			}
		}
	}

	m.emitEvent(ctx, domain.EventCronJobUpdated, *job)
	m.logger.Info("cron job updated", "id", id)

	job.NextRunAt = m.scheduler.GetNextRun(id)
	return job, nil
}

// Delete removes a cron job and its schedule.
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from scheduler (ignore error if not scheduled).
	m.scheduler.RemoveDynamicTask(id)

	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}

	m.emitEvent(ctx, domain.EventCronJobDeleted, map[string]string{"id": id})
	m.logger.Info("cron job deleted", "id", id)
	return nil
}

// ListRuns returns execution history for a job.
func (m *Manager) ListRuns(ctx context.Context, jobID string, limit int) ([]domain.CronRun, error) {
	return m.store.ListRuns(ctx, jobID, limit)
}

// LoadAndSchedule loads persisted jobs and schedules enabled ones.
// Should be called once during startup after SetHandler.
func (m *Manager) LoadAndSchedule(ctx context.Context) error {
	jobs, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("cronmanager: load: %w", err)
	}

	scheduled := 0
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}

		// Skip expired one-shot ("at") jobs — disable them instead of scheduling.
		if job.Schedule.Kind == "at" {
			if t, err := time.Parse(time.RFC3339, job.Schedule.At); err == nil && t.Before(time.Now()) {
				job.Enabled = false
				job.UpdatedAt = time.Now()
				m.store.Save(ctx, job)
				m.logger.Info("disabled expired one-shot job", "id", job.ID, "at", job.Schedule.At)
				continue
			}
		}

		if err := m.scheduleJob(job); err != nil {
			m.logger.Warn("failed to schedule persisted job", "id", job.ID, "error", err)
			continue
		}
		scheduled++
	}

	m.logger.Info("cron jobs loaded", "total", len(jobs), "scheduled", scheduled)
	return nil
}

// --- internal ---

func (m *Manager) scheduleJob(job domain.CronJob) error {
	sched, err := buildCronSchedule(job.Schedule)
	if err != nil {
		return err
	}

	oneShot := job.Schedule.Kind == "at"
	jobID := job.ID

	return m.scheduler.AddDynamicTask(jobID, sched, func(ctx context.Context) error {
		return m.executeJob(ctx, jobID)
	}, oneShot)
}

func (m *Manager) executeJob(ctx context.Context, jobID string) error {
	job, err := m.store.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("job %s not found: %w", jobID, err)
	}

	start := time.Now()
	var runErr error

	m.mu.Lock()
	handler := m.handler
	m.mu.Unlock()

	if handler == nil {
		m.logger.Warn("cron job skipped: handler not set", "id", jobID)
	} else if job.Action.Kind == "agent_run" {
		_, runErr = handler.Handle(ctx, domain.InboundMessage{
			SessionID:   "cron:" + jobID,
			Content:     job.Action.Message,
			ChannelName: "cron",
		})
	}

	duration := time.Since(start)

	// Record the run.
	run := domain.CronRun{
		JobID:     jobID,
		StartedAt: start,
		Duration:  duration.String(),
		Success:   runErr == nil,
	}
	if runErr != nil {
		run.Error = runErr.Error()
	}
	m.store.SaveRun(ctx, run)

	// Update job stats (and auto-disable one-shot jobs in same save).
	now := time.Now()
	job.LastRunAt = &now
	job.RunCount++
	job.UpdatedAt = now
	if job.Schedule.Kind == "at" {
		job.Enabled = false
	}
	m.store.Save(ctx, *job)

	m.emitEvent(ctx, domain.EventCronJobFired, run)

	return runErr
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

func (m *Manager) newID() string {
	t := time.Now()
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}

// validateCronSchedule validates a CronSchedule value.
func validateCronSchedule(s domain.CronSchedule) error {
	switch s.Kind {
	case "at":
		if s.At == "" {
			return fmt.Errorf("schedule kind 'at' requires 'at' field (ISO 8601 timestamp)")
		}
		if _, err := time.Parse(time.RFC3339, s.At); err != nil {
			return fmt.Errorf("invalid 'at' timestamp: %w", err)
		}
	case "every":
		if s.EveryMs <= 0 {
			return fmt.Errorf("schedule kind 'every' requires positive 'every_ms'")
		}
	case "cron":
		if s.Expression == "" {
			return fmt.Errorf("schedule kind 'cron' requires 'expression' field")
		}
		if _, err := scheduling.ParseSchedule(s.Expression); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	default:
		return fmt.Errorf("unknown schedule kind %q (want: at, every, cron)", s.Kind)
	}
	return nil
}

// buildCronSchedule converts a domain.CronSchedule into a cron.Schedule.
func buildCronSchedule(s domain.CronSchedule) (cron.Schedule, error) {
	switch s.Kind {
	case "at":
		t, err := time.Parse(time.RFC3339, s.At)
		if err != nil {
			return nil, fmt.Errorf("invalid at time: %w", err)
		}
		return &onceSchedule{at: t}, nil
	case "every":
		dur := time.Duration(s.EveryMs) * time.Millisecond
		return scheduling.NewConstantDelay(dur), nil
	case "cron":
		return scheduling.ParseSchedule(s.Expression)
	default:
		return nil, fmt.Errorf("unknown schedule kind: %s", s.Kind)
	}
}

// onceSchedule fires once at a specific time. Thread-safe via atomic.
type onceSchedule struct {
	at   time.Time
	done atomic.Bool
}

func (s *onceSchedule) Next(t time.Time) time.Time {
	if s.done.Load() || t.After(s.at) {
		s.done.Store(true)
		return time.Time{} // zero value = never fire again
	}
	s.done.Store(true)
	return s.at
}
