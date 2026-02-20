package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// NoteSummary describes a note's metadata.
type NoteSummary struct {
	Name      string    `json:"name"`
	Size      int       `json:"size_bytes"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NoteSearchResult is a single search hit within a note.
type NoteSearchResult struct {
	Name    string `json:"name"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// NotesBackend abstracts note storage operations.
type NotesBackend interface {
	Create(name, content string) error
	Read(name string) (string, error)
	Update(name, content string) error
	Delete(name string) error
	List() ([]NoteSummary, error)
	Search(query string) ([]NoteSearchResult, error)
}

// noteNameRegex validates note names: alphanumeric, hyphens, underscores.
var noteNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

const maxNoteNameLen = 128

// LocalNotesBackend stores notes as markdown files in a directory.
type LocalNotesBackend struct {
	dataDir string
}

// NewLocalNotesBackend creates a backend that stores notes in dataDir.
// The directory is created if it does not exist.
func NewLocalNotesBackend(dataDir string) (*LocalNotesBackend, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create notes dir: %w", err)
	}
	return &LocalNotesBackend{dataDir: dataDir}, nil
}

func (b *LocalNotesBackend) path(name string) string {
	return filepath.Join(b.dataDir, name+".md")
}

func (b *LocalNotesBackend) Create(name, content string) error {
	p := b.path(name)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("note %q already exists", name)
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

func (b *LocalNotesBackend) Read(name string) (string, error) {
	data, err := os.ReadFile(b.path(name))
	if os.IsNotExist(err) {
		return "", fmt.Errorf("note %q not found", name)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *LocalNotesBackend) Update(name, content string) error {
	p := b.path(name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return fmt.Errorf("note %q not found", name)
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

func (b *LocalNotesBackend) Delete(name string) error {
	p := b.path(name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return fmt.Errorf("note %q not found", name)
	}
	return os.Remove(p)
}

func (b *LocalNotesBackend) List() ([]NoteSummary, error) {
	entries, err := os.ReadDir(b.dataDir)
	if err != nil {
		return nil, err
	}
	var notes []NoteSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		notes = append(notes, NoteSummary{
			Name:      name,
			Size:      int(info.Size()),
			UpdatedAt: info.ModTime(),
		})
	}
	return notes, nil
}

func (b *LocalNotesBackend) Search(query string) ([]NoteSearchResult, error) {
	entries, err := os.ReadDir(b.dataDir)
	if err != nil {
		return nil, err
	}
	lowerQuery := strings.ToLower(query)
	var results []NoteSearchResult
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		f, err := os.Open(filepath.Join(b.dataDir, e.Name()))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), lowerQuery) {
				results = append(results, NoteSearchResult{
					Name:    name,
					Line:    lineNum,
					Snippet: line,
				})
			}
		}
		f.Close()
	}
	return results, nil
}

// NotesTool provides note management to the LLM.
type NotesTool struct {
	backend NotesBackend
	logger  *slog.Logger
}

// NewNotesTool creates a notes tool backed by the given NotesBackend.
func NewNotesTool(backend NotesBackend, logger *slog.Logger) *NotesTool {
	return &NotesTool{backend: backend, logger: logger}
}

func (t *NotesTool) Name() string { return "notes" }
func (t *NotesTool) Description() string {
	return "Create, read, update, delete, list, and search personal notes stored as markdown files."
}

func (t *NotesTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["create", "read", "update", "delete", "list", "search"],
					"description": "The notes action to perform"
				},
				"name": {
					"type": "string",
					"description": "Note name (alphanumeric, hyphens, underscores)"
				},
				"content": {
					"type": "string",
					"description": "Note content (for create/update)"
				},
				"query": {
					"type": "string",
					"description": "Search query string (for search action)"
				}
			},
			"required": ["action"]
		}`),
	}
}

type notesParams struct {
	Action  string `json:"action"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	Query   string `json:"query,omitempty"`
}

func (t *NotesTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.notes", t.logger, params,
		Dispatch(func(p notesParams) string { return p.Action }, ActionMap[notesParams]{
			"create": t.handleCreate,
			"read":   t.handleRead,
			"update": t.handleUpdate,
			"delete": t.handleDelete,
			"list":   t.handleList,
			"search": t.handleSearch,
		}),
	)
}

func (t *NotesTool) validateName(name string) error {
	if err := RequireField("name", name); err != nil {
		return err
	}
	if len(name) > maxNoteNameLen {
		return fmt.Errorf("name too long: %d chars (max %d)", len(name), maxNoteNameLen)
	}
	if !noteNameRegex.MatchString(name) {
		return fmt.Errorf("invalid note name %q: must be alphanumeric with hyphens/underscores, starting with alphanumeric", name)
	}
	return nil
}

func (t *NotesTool) handleCreate(_ context.Context, p notesParams) (any, error) {
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	if err := RequireField("content", p.Content); err != nil {
		return nil, err
	}
	if err := t.backend.Create(p.Name, p.Content); err != nil {
		return nil, err
	}
	t.logger.Debug("note created", "name", p.Name, "size", len(p.Content))
	return TextResult(fmt.Sprintf("Note %q created (%d bytes)", p.Name, len(p.Content))), nil
}

func (t *NotesTool) handleRead(_ context.Context, p notesParams) (any, error) {
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	content, err := t.backend.Read(p.Name)
	if err != nil {
		return nil, err
	}
	return TextResult(content), nil
}

func (t *NotesTool) handleUpdate(_ context.Context, p notesParams) (any, error) {
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	if err := RequireField("content", p.Content); err != nil {
		return nil, err
	}
	if err := t.backend.Update(p.Name, p.Content); err != nil {
		return nil, err
	}
	t.logger.Debug("note updated", "name", p.Name, "size", len(p.Content))
	return TextResult(fmt.Sprintf("Note %q updated (%d bytes)", p.Name, len(p.Content))), nil
}

func (t *NotesTool) handleDelete(_ context.Context, p notesParams) (any, error) {
	if err := t.validateName(p.Name); err != nil {
		return nil, err
	}
	if err := t.backend.Delete(p.Name); err != nil {
		return nil, err
	}
	t.logger.Debug("note deleted", "name", p.Name)
	return TextResult(fmt.Sprintf("Note %q deleted", p.Name)), nil
}

func (t *NotesTool) handleList(_ context.Context, _ notesParams) (any, error) {
	notes, err := t.backend.List()
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return TextResult("No notes found."), nil
	}
	return notes, nil
}

func (t *NotesTool) handleSearch(_ context.Context, p notesParams) (any, error) {
	if err := RequireField("query", p.Query); err != nil {
		return nil, err
	}
	results, err := t.backend.Search(p.Query)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return TextResult("No matches found."), nil
	}
	return results, nil
}
