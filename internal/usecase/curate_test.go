package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"alfred-ai/internal/domain"
)

// stubLLM is a minimal LLM provider for testing curation.
type stubLLM struct {
	response string
}

func (s *stubLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return &domain.ChatResponse{
		Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: s.response,
		},
	}, nil
}

func (s *stubLLM) Name() string { return "stub" }

// stubMemory is a minimal memory provider for testing curation.
type stubMemory struct {
	stored []domain.MemoryEntry
}

func (s *stubMemory) Store(_ context.Context, entry domain.MemoryEntry) error {
	s.stored = append(s.stored, entry)
	return nil
}

func (s *stubMemory) Query(_ context.Context, _ string, _ int) ([]domain.MemoryEntry, error) {
	return nil, nil
}

func (s *stubMemory) Delete(_ context.Context, _ string) error { return nil }

func (s *stubMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	return &domain.CurateResult{}, nil
}

func (s *stubMemory) Sync(_ context.Context) error { return nil }
func (s *stubMemory) Name() string                 { return "stub" }
func (s *stubMemory) IsAvailable() bool            { return true }

func curateTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCurator_ExtractKnowledge(t *testing.T) {
	mem := &stubMemory{}
	llm := &stubLLM{
		response: `POINT: User prefers Go with clean architecture pattern
TAGS: golang, architecture, clean-architecture
POINT: Project uses PostgreSQL for the main database
TAGS: postgresql, database`,
	}

	curator := NewCurator(mem, llm, curateTestLogger())
	ctx := context.Background()

	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "I prefer Go with clean architecture."},
		{Role: domain.RoleAssistant, Content: "Got it! I'll use Go with clean architecture."},
		{Role: domain.RoleUser, Content: "We use PostgreSQL for the main database."},
	}

	result, err := curator.CurateConversation(ctx, messages)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}

	if result.Stored != 2 {
		t.Errorf("expected 2 stored, got %d", result.Stored)
	}
	if len(mem.stored) != 2 {
		t.Fatalf("expected 2 entries in memory, got %d", len(mem.stored))
	}
	if mem.stored[0].Content != "User prefers Go with clean architecture pattern" {
		t.Errorf("unexpected content: %q", mem.stored[0].Content)
	}
	if len(mem.stored[0].Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(mem.stored[0].Tags))
	}
}

func TestCurator_NONEResponse(t *testing.T) {
	mem := &stubMemory{}
	llm := &stubLLM{response: "NONE"}

	curator := NewCurator(mem, llm, curateTestLogger())
	ctx := context.Background()

	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "Hello!"},
		{Role: domain.RoleAssistant, Content: "Hi there!"},
	}

	result, err := curator.CurateConversation(ctx, messages)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}

	if result.Stored != 0 {
		t.Errorf("expected 0 stored, got %d", result.Stored)
	}
	if result.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", result.Skipped)
	}
}

func TestCurator_EmptyConversation(t *testing.T) {
	mem := &stubMemory{}
	llm := &stubLLM{response: ""}

	curator := NewCurator(mem, llm, curateTestLogger())
	ctx := context.Background()

	result, err := curator.CurateConversation(ctx, nil)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}

	if result.Stored != 0 {
		t.Errorf("expected 0 stored, got %d", result.Stored)
	}
}

func TestParseCurateResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "single point",
			input:    "POINT: User likes Go\nTAGS: golang",
			expected: 1,
		},
		{
			name:     "multiple points",
			input:    "POINT: Fact one\nTAGS: tag1\nPOINT: Fact two\nTAGS: tag2, tag3",
			expected: 2,
		},
		{
			name:     "NONE response",
			input:    "NONE",
			expected: 0,
		},
		{
			name:     "empty",
			input:    "",
			expected: 0,
		},
		{
			name:     "malformed - no TAGS",
			input:    "POINT: Missing tags line\nSomething else",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points := parseCurateResponse(tt.input)
			if len(points) != tt.expected {
				t.Errorf("expected %d points, got %d", tt.expected, len(points))
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tags := parseTags("  Golang , Architecture , clean-architecture  ")
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "golang" {
		t.Errorf("tags[0] = %q, want %q", tags[0], "golang")
	}
	if tags[2] != "clean-architecture" {
		t.Errorf("tags[2] = %q, want %q", tags[2], "clean-architecture")
	}
}

// --- Additional coverage tests ---

type errorLLM struct{}

func (m *errorLLM) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	return nil, fmt.Errorf("llm error")
}
func (m *errorLLM) Name() string { return "error-llm" }

type errorStoreMemory struct {
	mockMemory
}

func (m *errorStoreMemory) Store(_ context.Context, _ domain.MemoryEntry) error {
	return fmt.Errorf("store failed")
}

func TestCuratorEmptyMessages(t *testing.T) {
	c := NewCurator(&mockMemory{}, &mockLLM{}, newTestLogger())
	result, err := c.CurateConversation(context.Background(), nil)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}
	if result.Stored != 0 {
		t.Errorf("Stored = %d", result.Stored)
	}
}

func TestCuratorSystemOnlyMessages(t *testing.T) {
	c := NewCurator(&mockMemory{}, &mockLLM{}, newTestLogger())
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "system prompt"},
		{Role: domain.RoleTool, Content: "tool result"},
	}
	result, err := c.CurateConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}
	if result.Stored != 0 {
		t.Errorf("Stored = %d, expected 0 (all messages skipped)", result.Stored)
	}
}

func TestCuratorLLMError(t *testing.T) {
	llm := &errorLLM{}
	c := NewCurator(&mockMemory{}, llm, newTestLogger())
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "important fact"},
	}
	_, err := c.CurateConversation(context.Background(), msgs)
	if err == nil {
		t.Error("expected error from LLM failure")
	}
}

func TestCuratorNONEResponse(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "NONE"}},
	}}
	c := NewCurator(&mockMemory{}, llm, newTestLogger())
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "hi"},
	}
	result, err := c.CurateConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestCuratorExtractsKnowledge(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "POINT: User prefers Go\nTAGS: golang, preference\nPOINT: User uses vim\nTAGS: editor, vim"}},
	}}
	c := NewCurator(&mockMemory{}, llm, newTestLogger())
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "I use Go and vim"},
		{Role: domain.RoleAssistant, Content: "Nice choices!"},
	}
	result, err := c.CurateConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}
	if result.Stored != 2 {
		t.Errorf("Stored = %d, want 2", result.Stored)
	}
}

func TestCuratorStoreError(t *testing.T) {
	llm := &mockLLM{responses: []domain.ChatResponse{
		{Message: domain.Message{Role: domain.RoleAssistant, Content: "POINT: fact\nTAGS: test"}},
	}}
	mem := &errorStoreMemory{}
	c := NewCurator(mem, llm, newTestLogger())
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "test"},
	}
	result, err := c.CurateConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("CurateConversation: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (store error)", result.Skipped)
	}
}

func TestParseCurateResponseEmpty(t *testing.T) {
	points := parseCurateResponse("")
	if len(points) != 0 {
		t.Errorf("expected 0 points, got %d", len(points))
	}
}

func TestParseCurateResponseNONE(t *testing.T) {
	points := parseCurateResponse("NONE")
	if len(points) != 0 {
		t.Errorf("expected 0 points, got %d", len(points))
	}
}

func TestParseTagsMultiple(t *testing.T) {
	tags := parseTags("go, rust, python")
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "go" || tags[1] != "rust" || tags[2] != "python" {
		t.Errorf("tags = %v", tags)
	}
}

func TestUniqueStrings(t *testing.T) {
	result := uniqueStrings([]string{"a", "b", "a", "c", "b"})
	if len(result) != 3 {
		t.Errorf("expected 3 unique strings, got %d", len(result))
	}
}

// stubEmbedder is a minimal EmbeddingProvider for testing.
type stubEmbedder struct{}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, 3)
	}
	return out, nil
}
func (s *stubEmbedder) Dimensions() int { return 3 }
func (s *stubEmbedder) Name() string    { return "stub" }

func TestCuratorWithEmbedderOption(t *testing.T) {
	emb := &stubEmbedder{}
	c := NewCurator(&mockMemory{}, &mockLLM{}, newTestLogger(), WithEmbedder(emb))
	if c.embedder == nil {
		t.Error("expected embedder to be set")
	}
	if c.embedder.Name() != "stub" {
		t.Errorf("embedder name = %q, want stub", c.embedder.Name())
	}
}

func TestCuratorWithoutEmbedderOption(t *testing.T) {
	c := NewCurator(&mockMemory{}, &mockLLM{}, newTestLogger())
	if c.embedder != nil {
		t.Error("expected embedder to be nil by default")
	}
}
