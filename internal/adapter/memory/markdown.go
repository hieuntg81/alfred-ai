package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"alfred-ai/internal/domain"
)

// markdownFrontmatter is the YAML structure embedded in each .md file.
type markdownFrontmatter struct {
	ID        string            `yaml:"id"`
	Tags      []string          `yaml:"tags"`
	CreatedAt string            `yaml:"created_at"`
	UpdatedAt string            `yaml:"updated_at"`
	Metadata  map[string]string `yaml:"metadata,omitempty"`
}

// MarkdownOption configures MarkdownMemory.
type MarkdownOption func(*MarkdownMemory)

// WithEncryptor sets a content encryptor for at-rest encryption.
func WithEncryptor(enc domain.ContentEncryptor) MarkdownOption {
	return func(m *MarkdownMemory) {
		m.encryptor = enc
	}
}

// MarkdownMemory implements domain.MemoryProvider using local markdown files.
type MarkdownMemory struct {
	dataDir    string
	entriesDir string
	index      *MemoryIndex
	encryptor  domain.ContentEncryptor
}

// NewMarkdownMemory creates a markdown-based memory provider.
func NewMarkdownMemory(dataDir string, opts ...MarkdownOption) (*MarkdownMemory, error) {
	entriesDir := filepath.Join(dataDir, "entries")
	if err := os.MkdirAll(entriesDir, 0700); err != nil {
		return nil, fmt.Errorf("create entries dir: %w", err)
	}

	idx, err := NewMemoryIndex(dataDir)
	if err != nil {
		return nil, fmt.Errorf("init index: %w", err)
	}

	m := &MarkdownMemory{
		dataDir:    dataDir,
		entriesDir: entriesDir,
		index:      idx,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

func (m *MarkdownMemory) Store(_ context.Context, entry domain.MemoryEntry) error {
	if entry.ID == "" {
		id, err := generateID()
		if err != nil {
			return domain.NewDomainError("MarkdownMemory.Store", domain.ErrMemoryStore, err.Error())
		}
		entry.ID = id
	}

	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	// Build plaintext preview BEFORE encryption for the index
	preview := entry.Content
	if len(preview) > 200 {
		preview = preview[:200]
	}

	// Write the .md file (with optional encryption of body)
	content := m.renderEntry(entry)
	filename := fmt.Sprintf("%s-%s.md", entry.CreatedAt.Format("2006-01-02"), entry.ID)
	path := filepath.Join(m.entriesDir, filename)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return domain.NewDomainError("MarkdownMemory.Store", domain.ErrMemoryStore, err.Error())
	}

	// Update index with plaintext preview
	if err := m.index.Add(IndexEntry{
		ID:             entry.ID,
		Filename:       filename,
		Tags:           entry.Tags,
		ContentPreview: preview,
		CreatedAt:      entry.CreatedAt,
	}); err != nil {
		return domain.NewDomainError("MarkdownMemory.Store", domain.ErrMemoryIndex, err.Error())
	}

	return nil
}

func (m *MarkdownMemory) Query(_ context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	matches := m.index.Search(query, limit)
	if len(matches) == 0 {
		return nil, nil
	}

	entries := make([]domain.MemoryEntry, 0, len(matches))
	for _, match := range matches {
		path := filepath.Join(m.entriesDir, match.Filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip missing files
		}

		entry, err := m.parseEntry(data)
		if err != nil {
			continue // skip malformed files
		}
		entries = append(entries, *entry)
	}

	return entries, nil
}

func (m *MarkdownMemory) Delete(_ context.Context, id string) error {
	filename := m.index.GetFilename(id)
	if filename == "" {
		return domain.NewDomainError("MarkdownMemory.Delete", domain.ErrMemoryDelete,
			fmt.Sprintf("entry %s not found", id))
	}

	// Remove the file
	path := filepath.Join(m.entriesDir, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return domain.NewDomainError("MarkdownMemory.Delete", domain.ErrMemoryDelete, err.Error())
	}

	// Remove from index
	if err := m.index.Remove(id); err != nil {
		return domain.NewDomainError("MarkdownMemory.Delete", domain.ErrMemoryIndex, err.Error())
	}

	return nil
}

func (m *MarkdownMemory) Curate(_ context.Context, _ []domain.Message) (*domain.CurateResult, error) {
	// Curation is handled externally by the Curator usecase
	return &domain.CurateResult{}, nil
}

func (m *MarkdownMemory) Sync(_ context.Context) error {
	return nil // local-only, no sync needed
}

func (m *MarkdownMemory) Name() string      { return "markdown" }
func (m *MarkdownMemory) IsAvailable() bool { return true }

// renderEntry produces a markdown file with optional body encryption.
func (m *MarkdownMemory) renderEntry(entry domain.MemoryEntry) string {
	body := entry.Content
	if m.encryptor != nil {
		encrypted, err := m.encryptor.Encrypt(body)
		if err == nil {
			body = encrypted
		}
	}

	fm := markdownFrontmatter{
		ID:        entry.ID,
		Tags:      entry.Tags,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt: entry.UpdatedAt.Format(time.RFC3339),
		Metadata:  entry.Metadata,
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	enc.Encode(fm)
	enc.Close()
	buf.WriteString("---\n\n")
	buf.WriteString(body)
	buf.WriteByte('\n')

	return buf.String()
}

// parseEntry reads a markdown file and decrypts body if needed.
func (m *MarkdownMemory) parseEntry(data []byte) (*domain.MemoryEntry, error) {
	entry, err := parseMarkdown(data)
	if err != nil {
		return nil, err
	}

	if m.encryptor != nil {
		decrypted, err := m.encryptor.Decrypt(entry.Content)
		if err != nil {
			return nil, fmt.Errorf("decrypt content: %w", err)
		}
		entry.Content = decrypted
	}

	return entry, nil
}

// renderMarkdown produces a markdown file with YAML frontmatter (no encryption).
func renderMarkdown(entry domain.MemoryEntry) string {
	fm := markdownFrontmatter{
		ID:        entry.ID,
		Tags:      entry.Tags,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339),
		UpdatedAt: entry.UpdatedAt.Format(time.RFC3339),
		Metadata:  entry.Metadata,
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	enc.Encode(fm)
	enc.Close()
	buf.WriteString("---\n\n")
	buf.WriteString(entry.Content)
	buf.WriteByte('\n')

	return buf.String()
}

// parseMarkdown reads a markdown file with YAML frontmatter (no decryption).
func parseMarkdown(data []byte) (*domain.MemoryEntry, error) {
	content := string(data)

	// Split on "---" delimiters
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing frontmatter start")
	}

	rest := content[4:] // skip first "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return nil, fmt.Errorf("missing frontmatter end")
	}

	fmRaw := rest[:idx]
	body := strings.TrimSpace(rest[idx+5:]) // skip "\n---\n"

	var fm markdownFrontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339, fm.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, fm.UpdatedAt)

	return &domain.MemoryEntry{
		ID:        fm.ID,
		Content:   body,
		Tags:      fm.Tags,
		Metadata:  fm.Metadata,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// generateID returns a short random hex ID (8 bytes = 16 hex chars).
func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
