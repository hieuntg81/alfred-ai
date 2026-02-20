package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileCallStore implements CallPersistence using a JSONL append-only file.
// Each state change appends the full CallRecord as a JSON line.
// On load, the last entry for each call ID wins (like an event log).
type FileCallStore struct {
	mu   sync.Mutex
	dir  string
	file *os.File
}

// NewFileCallStore creates a new file-backed call store.
// It creates the directory and opens (or creates) the JSONL file.
func NewFileCallStore(dir string) (*FileCallStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("voice call store: create dir: %w", err)
	}

	path := filepath.Join(dir, "calls.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("voice call store: open file: %w", err)
	}

	return &FileCallStore{dir: dir, file: f}, nil
}

// Append writes a call record as a single JSON line to the JSONL file.
func (s *FileCallStore) Append(record CallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("voice call store: marshal: %w", err)
	}

	data = append(data, '\n')
	if _, err := s.file.Write(data); err != nil {
		return fmt.Errorf("voice call store: write: %w", err)
	}

	return nil
}

// Load reads the JSONL file and returns the latest state of each call.
// The last entry for each call ID wins, providing event-sourced state.
func (s *FileCallStore) Load() ([]CallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, "calls.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("voice call store: open for read: %w", err)
	}
	defer f.Close()

	latest := make(map[string]CallRecord)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB lines

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var record CallRecord
		if err := json.Unmarshal(line, &record); err != nil {
			// Skip corrupt lines — best effort recovery.
			continue
		}
		if record.ID == "" {
			continue
		}
		latest[record.ID] = record
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("voice call store: scan: %w", err)
	}

	records := make([]CallRecord, 0, len(latest))
	for _, r := range latest {
		records = append(records, r)
	}
	return records, nil
}

// Close closes the underlying file.
func (s *FileCallStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		return s.file.Close()
	}
	return nil
}
