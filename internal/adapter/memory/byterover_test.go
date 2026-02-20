package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
)

// outOfSyncMockClient is a mock that returns not-in-sync status.
type outOfSyncMockClient struct {
	*MockByteRoverClient
}

func (m *outOfSyncMockClient) SyncStatus(_ context.Context) (*SyncStatus, error) {
	return &SyncStatus{
		LastSyncAt:  time.Now(),
		InSync:      false,
		PendingPull: 1,
	}, nil
}

// failEncryptor always fails on Encrypt.
type failEncryptor struct{}

func (e *failEncryptor) Encrypt(_ string) (string, error) { return "", fmt.Errorf("encrypt failed") }
func (e *failEncryptor) Decrypt(s string) (string, error) { return s, nil }
func (e *failEncryptor) IsEncrypted(_ string) bool        { return false }
func (e *failEncryptor) Rotate(_ string) error            { return nil }

// failDecryptEncryptor always fails on Decrypt.
type failDecryptEncryptor struct{}

func (e *failDecryptEncryptor) Encrypt(s string) (string, error) { return s, nil }
func (e *failDecryptEncryptor) Decrypt(_ string) (string, error) {
	return "", fmt.Errorf("decrypt failed")
}
func (e *failDecryptEncryptor) IsEncrypted(_ string) bool { return false }
func (e *failDecryptEncryptor) Rotate(_ string) error     { return nil }

// errorWriteMockClient fails on WriteContext.
type errorWriteMockClient struct{ MockByteRoverClient }

func (m *errorWriteMockClient) WriteContext(_ context.Context, _, _ string, _ []string, _ map[string]string) error {
	return fmt.Errorf("write error")
}

// errorQueryMockClient fails on Query.
type errorQueryMockClient struct{ MockByteRoverClient }

func (m *errorQueryMockClient) Query(_ context.Context, _ string, _ int) ([]ByteRoverResult, error) {
	return nil, fmt.Errorf("query error")
}

// errorDeleteMockClient fails on DeleteContext.
type errorDeleteMockClient struct{ MockByteRoverClient }

func (m *errorDeleteMockClient) DeleteContext(_ context.Context, _ string) error {
	return fmt.Errorf("delete error")
}

// errorSyncMockClient fails on SyncStatus.
type errorSyncMockClient struct{ MockByteRoverClient }

func (m *errorSyncMockClient) SyncStatus(_ context.Context) (*SyncStatus, error) {
	return nil, fmt.Errorf("sync error")
}

// errorPullMockClient returns not-in-sync then fails on Pull.
type errorPullMockClient struct{ *MockByteRoverClient }

func (m *errorPullMockClient) SyncStatus(_ context.Context) (*SyncStatus, error) {
	return &SyncStatus{InSync: false, PendingPull: 1}, nil
}
func (m *errorPullMockClient) Pull(_ context.Context, _ time.Time) ([]ByteRoverResult, error) {
	return nil, fmt.Errorf("pull error")
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestByteRoverMemory_StoreAndQuery(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()

	entry := domain.MemoryEntry{
		Content:  "User prefers Rust for systems programming.",
		Tags:     []string{"rust", "systems"},
		Metadata: map[string]string{"source": "conversation"},
	}

	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if mock.EntryCount() != 1 {
		t.Fatalf("expected 1 entry in mock, got %d", mock.EntryCount())
	}

	results, err := mem.Query(ctx, "rust", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != entry.Content {
		t.Errorf("content mismatch: got %q", results[0].Content)
	}
}

func TestByteRoverMemory_QueryNoMatch(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()

	entry := domain.MemoryEntry{
		Content: "User likes Python.",
		Tags:    []string{"python"},
	}
	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	results, err := mem.Query(ctx, "javascript", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestByteRoverMemory_Sync(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()

	if err := mem.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestByteRoverMemory_Name(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	if mem.Name() != "byterover" {
		t.Errorf("Name() = %q, want %q", mem.Name(), "byterover")
	}
	if !mem.IsAvailable() {
		t.Error("IsAvailable() = false, want true")
	}
}

func TestByteRoverMemory_StoreGeneratesID(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()

	entry := domain.MemoryEntry{
		Content: "Entry without ID.",
		Tags:    []string{"test"},
	}

	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if mock.EntryCount() != 1 {
		t.Fatalf("expected 1 entry, got %d", mock.EntryCount())
	}
}

func TestByteRoverMemory_WithEncryptor(t *testing.T) {
	mock := NewMockByteRoverClient()
	enc, err := security.NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	mem := NewByteRoverMemory(mock, testLogger(), WithByteRoverEncryptor(enc))

	ctx := context.Background()
	entry := domain.MemoryEntry{
		ID:      "enc-test",
		Content: "Secret content for encryption test.",
		Tags:    []string{"secret"},
	}

	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Read raw mock entry - should be encrypted
	raw, err := mock.ReadContext(ctx, "enc-test")
	if err != nil {
		t.Fatalf("ReadContext: %v", err)
	}
	if raw.Content == entry.Content {
		t.Error("raw content should be encrypted, not plaintext")
	}

	// Query should return decrypted content
	results, err := mem.Query(ctx, "secret", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != entry.Content {
		t.Errorf("decrypted content = %q, want %q", results[0].Content, entry.Content)
	}
}

func TestByteRoverMemory_Curate(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	result, err := mem.Curate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if result == nil {
		t.Error("Curate returned nil result")
	}
}

func TestByteRoverMemory_DeleteNonExistent(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	// Delete non-existent entry - mock just does delete(map, key), no error
	err := mem.Delete(context.Background(), "nonexistent")
	if err != nil {
		t.Logf("Delete error (expected flow): %v", err)
	}
}

func TestByteRoverMemory_SyncNotInSync(t *testing.T) {
	mock := &outOfSyncMockClient{MockByteRoverClient: NewMockByteRoverClient()}
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()
	if err := mem.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestMockClient_Authenticate(t *testing.T) {
	mock := NewMockByteRoverClient()
	if err := mock.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestMockClient_ReadContext(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	mock.WriteContext(ctx, "read-test", "content", []string{"tag"}, nil)

	result, err := mock.ReadContext(ctx, "read-test")
	if err != nil {
		t.Fatalf("ReadContext: %v", err)
	}
	if result.Content != "content" {
		t.Errorf("Content = %q", result.Content)
	}

	_, err = mock.ReadContext(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent entry")
	}
}

func TestMockClient_Push(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	entries := []ByteRoverResult{
		{ID: "push-1", Content: "pushed content"},
	}
	if err := mock.Push(ctx, entries); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if mock.PushCount() != 1 {
		t.Errorf("PushCount = %d, want 1", mock.PushCount())
	}
	if mock.EntryCount() != 1 {
		t.Errorf("EntryCount = %d, want 1", mock.EntryCount())
	}
}

func TestMockClient_Pull(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	mock.WriteContext(ctx, "pull-1", "content", nil, nil)

	results, err := mock.Pull(ctx, time.Time{})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Pull returned %d results, want 1", len(results))
	}
}

func TestByteRoverMemory_StoreEncryptError(t *testing.T) {
	mock := NewMockByteRoverClient()
	enc := &failEncryptor{}
	mem := NewByteRoverMemory(mock, testLogger(), WithByteRoverEncryptor(enc))

	err := mem.Store(context.Background(), domain.MemoryEntry{
		ID:      "err-test",
		Content: "test",
	})
	if err == nil {
		t.Error("expected error from encrypt failure")
	}
}

func TestByteRoverMemory_QueryDecryptError(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	// Store raw encrypted-looking content directly in mock
	mock.WriteContext(ctx, "q-enc", "enc:corrupted-data", []string{"test"}, nil)

	// Use an encryptor that fails on decrypt
	enc := &failDecryptEncryptor{}
	mem := NewByteRoverMemory(mock, testLogger(), WithByteRoverEncryptor(enc))

	_, err := mem.Query(ctx, "test", 10)
	if err == nil {
		t.Error("expected error from decrypt failure")
	}
}

func TestByteRoverMemory_StoreWriteError(t *testing.T) {
	mock := &errorWriteMockClient{}
	mem := NewByteRoverMemory(mock, testLogger())

	err := mem.Store(context.Background(), domain.MemoryEntry{
		ID:      "err-test",
		Content: "test",
	})
	if err == nil {
		t.Error("expected error from WriteContext failure")
	}
}

func TestByteRoverMemory_QueryError(t *testing.T) {
	mock := &errorQueryMockClient{}
	mem := NewByteRoverMemory(mock, testLogger())

	_, err := mem.Query(context.Background(), "test", 10)
	if err == nil {
		t.Error("expected error from Query failure")
	}
}

func TestByteRoverMemory_DeleteError(t *testing.T) {
	mock := &errorDeleteMockClient{}
	mem := NewByteRoverMemory(mock, testLogger())

	err := mem.Delete(context.Background(), "test")
	if err == nil {
		t.Error("expected error from DeleteContext failure")
	}
}

func TestByteRoverMemory_SyncStatusError(t *testing.T) {
	mock := &errorSyncMockClient{}
	mem := NewByteRoverMemory(mock, testLogger())

	err := mem.Sync(context.Background())
	if err == nil {
		t.Error("expected error from SyncStatus failure")
	}
}

func TestByteRoverMemory_SyncPullError(t *testing.T) {
	mock := &errorPullMockClient{MockByteRoverClient: NewMockByteRoverClient()}
	mem := NewByteRoverMemory(mock, testLogger())

	err := mem.Sync(context.Background())
	if err == nil {
		t.Error("expected error from Pull failure")
	}
}

func TestByteRoverMemory_Delete(t *testing.T) {
	mock := NewMockByteRoverClient()
	mem := NewByteRoverMemory(mock, testLogger())

	ctx := context.Background()

	entry := domain.MemoryEntry{
		ID:      "del-test",
		Content: "To be deleted.",
		Tags:    []string{"test"},
	}
	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if mock.EntryCount() != 1 {
		t.Fatalf("expected 1 entry, got %d", mock.EntryCount())
	}

	if err := mem.Delete(ctx, "del-test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mock.EntryCount() != 0 {
		t.Errorf("expected 0 entries after delete, got %d", mock.EntryCount())
	}
}

// --- Additional tests for coverage ---

// TestMockClient_QueryTagMatch covers the tag-search branch in mock Query
// where content does NOT match but a tag matches the query.
func TestMockClient_QueryTagMatch(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	// Store an entry whose Content does NOT contain the query keyword,
	// but one of the Tags does.
	mock.WriteContext(ctx, "tag-match", "completely unrelated content", []string{"golang", "testing"}, nil)

	results, err := mock.Query(ctx, "golang", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from tag match, got %d", len(results))
	}
	if results[0].ID != "tag-match" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "tag-match")
	}
}

// TestMockClient_QueryLimitApplied covers the limit branch in mock Query.
func TestMockClient_QueryLimitApplied(t *testing.T) {
	mock := NewMockByteRoverClient()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		mock.WriteContext(ctx, fmt.Sprintf("lim-%d", i), "matching content", nil, nil)
	}

	results, err := mock.Query(ctx, "matching", 2)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}
}
