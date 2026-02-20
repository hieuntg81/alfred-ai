package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockByteRoverClient is an in-memory implementation of ByteRoverClient for testing.
type MockByteRoverClient struct {
	mu            sync.RWMutex
	entries       map[string]ByteRoverResult
	authenticated bool
	pushLog       [][]ByteRoverResult
}

// NewMockByteRoverClient creates a mock ByteRover client.
func NewMockByteRoverClient() *MockByteRoverClient {
	return &MockByteRoverClient{
		entries: make(map[string]ByteRoverResult),
	}
}

func (m *MockByteRoverClient) Authenticate(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authenticated = true
	return nil
}

func (m *MockByteRoverClient) WriteContext(_ context.Context, id, content string, tags []string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	m.entries[id] = ByteRoverResult{
		ID:        id,
		Content:   content,
		Tags:      tags,
		Metadata:  metadata,
		Score:     1.0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return nil
}

func (m *MockByteRoverClient) ReadContext(_ context.Context, id string) (*ByteRoverResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.entries[id]
	if !ok {
		return nil, fmt.Errorf("entry %s not found", id)
	}
	return &entry, nil
}

func (m *MockByteRoverClient) DeleteContext(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, id)
	return nil
}

func (m *MockByteRoverClient) Query(_ context.Context, query string, limit int) ([]ByteRoverResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []ByteRoverResult

	for _, entry := range m.entries {
		if strings.Contains(strings.ToLower(entry.Content), queryLower) {
			results = append(results, entry)
		} else {
			for _, tag := range entry.Tags {
				if strings.Contains(strings.ToLower(tag), queryLower) {
					results = append(results, entry)
					break
				}
			}
		}
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *MockByteRoverClient) Push(_ context.Context, entries []ByteRoverResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pushLog = append(m.pushLog, entries)
	for _, e := range entries {
		m.entries[e.ID] = e
	}
	return nil
}

func (m *MockByteRoverClient) Pull(_ context.Context, since time.Time) ([]ByteRoverResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []ByteRoverResult
	for _, entry := range m.entries {
		if entry.UpdatedAt.After(since) {
			results = append(results, entry)
		}
	}
	return results, nil
}

func (m *MockByteRoverClient) SyncStatus(_ context.Context) (*SyncStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &SyncStatus{
		LastSyncAt: time.Now(),
		InSync:     true,
	}, nil
}

// PushCount returns the number of Push calls made (for test assertions).
func (m *MockByteRoverClient) PushCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pushLog)
}

// EntryCount returns the number of stored entries (for test assertions).
func (m *MockByteRoverClient) EntryCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
