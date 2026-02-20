package cronjob

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"alfred-ai/internal/domain"
)

const maxRunsPerJob = 100

// FileStore implements domain.CronStore with JSON file persistence.
type FileStore struct {
	dir  string
	mu   sync.RWMutex
	jobs map[string]domain.CronJob
	runs map[string][]domain.CronRun // jobID → runs
}

// NewFileStore creates a new file-backed cron store.
// It loads existing data from the directory on creation.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("cronstore: create dir: %w", err)
	}

	s := &FileStore{
		dir:  dir,
		jobs: make(map[string]domain.CronJob),
		runs: make(map[string][]domain.CronRun),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("cronstore: load: %w", err)
	}

	return s, nil
}

func (s *FileStore) Save(_ context.Context, job domain.CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[job.ID] = job
	return s.saveJobs()
}

func (s *FileStore) Get(_ context.Context, id string) (*domain.CronJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("cronstore: job %q not found", id)
	}
	return &job, nil
}

func (s *FileStore) List(_ context.Context) ([]domain.CronJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]domain.CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs, nil
}

func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("cronstore: job %q not found", id)
	}
	delete(s.jobs, id)
	delete(s.runs, id)

	if err := s.saveJobs(); err != nil {
		return err
	}
	return s.saveRuns()
}

func (s *FileStore) SaveRun(_ context.Context, run domain.CronRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runs := s.runs[run.JobID]
	runs = append(runs, run)
	if len(runs) > maxRunsPerJob {
		runs = runs[len(runs)-maxRunsPerJob:]
	}
	s.runs[run.JobID] = runs
	return s.saveRuns()
}

func (s *FileStore) ListRuns(_ context.Context, jobID string, limit int) ([]domain.CronRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runs := s.runs[jobID]
	if limit <= 0 || limit > len(runs) {
		limit = len(runs)
	}
	// Return most recent first.
	start := len(runs) - limit
	result := make([]domain.CronRun, limit)
	for i := 0; i < limit; i++ {
		result[i] = runs[start+limit-1-i]
	}
	return result, nil
}

// --- persistence ---

func (s *FileStore) jobsPath() string { return filepath.Join(s.dir, "jobs.json") }
func (s *FileStore) runsPath() string { return filepath.Join(s.dir, "runs.json") }

func (s *FileStore) load() error {
	if data, err := os.ReadFile(s.jobsPath()); err == nil {
		var jobs []domain.CronJob
		if err := json.Unmarshal(data, &jobs); err != nil {
			return fmt.Errorf("parse jobs.json: %w", err)
		}
		for _, j := range jobs {
			s.jobs[j.ID] = j
		}
	}

	if data, err := os.ReadFile(s.runsPath()); err == nil {
		if err := json.Unmarshal(data, &s.runs); err != nil {
			return fmt.Errorf("parse runs.json: %w", err)
		}
	}

	return nil
}

func (s *FileStore) saveJobs() error {
	jobs := make([]domain.CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return writeJSON(s.jobsPath(), jobs)
}

func (s *FileStore) saveRuns() error {
	return writeJSON(s.runsPath(), s.runs)
}

// writeJSON atomically writes v as indented JSON to path.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return domain.WrapOp("marshal", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return domain.WrapOp("write", err)
	}
	return os.Rename(tmp, path)
}
