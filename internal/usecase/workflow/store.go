package workflow

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

const maxWorkflowRuns = 100

// FileStore implements domain.WorkflowStore with JSON file persistence.
type FileStore struct {
	dir  string
	mu   sync.RWMutex
	runs map[string]domain.WorkflowRun
}

// NewFileStore creates a new file-backed workflow store.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("workflowstore: create dir: %w", err)
	}

	s := &FileStore{
		dir:  dir,
		runs: make(map[string]domain.WorkflowRun),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("workflowstore: load: %w", err)
	}

	return s, nil
}

func (s *FileStore) SaveRun(_ context.Context, run domain.WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runs[run.ID] = run

	// Evict oldest runs if over limit.
	if len(s.runs) > maxWorkflowRuns {
		s.evictOldest()
	}

	return s.persist()
}

func (s *FileStore) GetRun(_ context.Context, id string) (*domain.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	run, ok := s.runs[id]
	if !ok {
		return nil, fmt.Errorf("workflowstore: run %q not found", id)
	}
	return &run, nil
}

func (s *FileStore) ListRuns(_ context.Context, limit int) ([]domain.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runs := make([]domain.WorkflowRun, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt) // newest first
	})

	if limit > 0 && limit < len(runs) {
		runs = runs[:limit]
	}
	return runs, nil
}

func (s *FileStore) DeleteRun(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.runs[id]; !ok {
		return fmt.Errorf("workflowstore: run %q not found", id)
	}
	delete(s.runs, id)
	return s.persist()
}

func (s *FileStore) GetRunByToken(_ context.Context, token string) (*domain.WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.runs {
		if r.ResumeToken == token && r.Status == "paused" {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("workflowstore: no paused run with token %q", token)
}

// --- persistence ---

func (s *FileStore) runsPath() string {
	return filepath.Join(s.dir, "workflow_runs.json")
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.runsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return domain.WrapOp("read", err)
	}

	var runs []domain.WorkflowRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return fmt.Errorf("parse workflow_runs.json: %w", err)
	}
	for _, r := range runs {
		s.runs[r.ID] = r
	}
	return nil
}

func (s *FileStore) persist() error {
	runs := make([]domain.WorkflowRun, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, r)
	}
	return writeJSON(s.runsPath(), runs)
}

// evictOldest removes the oldest completed/failed runs until count <= maxWorkflowRuns.
func (s *FileStore) evictOldest() {
	type entry struct {
		id string
		t  domain.WorkflowRun
	}
	var candidates []entry
	for id, r := range s.runs {
		if r.Status == "completed" || r.Status == "failed" || r.Status == "denied" {
			candidates = append(candidates, entry{id, r})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].t.CreatedAt.Before(candidates[j].t.CreatedAt)
	})
	for _, c := range candidates {
		if len(s.runs) <= maxWorkflowRuns {
			break
		}
		delete(s.runs, c.id)
	}
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
