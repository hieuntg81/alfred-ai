package domain

import (
	"context"
	"time"
)

// CronJob represents a runtime-created scheduled job managed by the LLM.
type CronJob struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Schedule  CronSchedule      `json:"schedule"`
	Action    CronAction        `json:"action"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	LastRunAt *time.Time        `json:"last_run_at,omitempty"`
	NextRunAt *time.Time        `json:"next_run_at,omitempty"`
	RunCount  int               `json:"run_count"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// CronSchedule supports three kinds: "at" (one-shot), "every" (interval), "cron" (expression).
type CronSchedule struct {
	Kind       string `json:"kind"`                 // "at", "every", "cron"
	At         string `json:"at,omitempty"`         // ISO 8601 timestamp for one-shot
	EveryMs    int64  `json:"every_ms,omitempty"`   // interval in milliseconds
	Expression string `json:"expression,omitempty"` // cron expression e.g. "*/5 * * * *"
}

// CronAction defines what a job does when triggered.
type CronAction struct {
	Kind    string `json:"kind"` // "agent_run"
	AgentID string `json:"agent_id,omitempty"`
	Channel string `json:"channel,omitempty"`
	Message string `json:"message"`
}

// CronRun records one execution of a cron job.
type CronRun struct {
	JobID     string    `json:"job_id"`
	StartedAt time.Time `json:"started_at"`
	Duration  string    `json:"duration"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// CronStore provides persistent storage for cron jobs and their execution history.
type CronStore interface {
	Save(ctx context.Context, job CronJob) error
	Get(ctx context.Context, id string) (*CronJob, error)
	List(ctx context.Context) ([]CronJob, error)
	Delete(ctx context.Context, id string) error
	SaveRun(ctx context.Context, run CronRun) error
	ListRuns(ctx context.Context, jobID string, limit int) ([]CronRun, error)
}
