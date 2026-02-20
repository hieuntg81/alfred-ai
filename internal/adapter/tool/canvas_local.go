package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// LocalCanvasBackend stores canvases on the local filesystem.
// Directory structure: <root>/<session_id>/<canvas_name>/index.html
type LocalCanvasBackend struct {
	root string
}

// NewLocalCanvasBackend creates a local canvas backend rooted at the given directory.
func NewLocalCanvasBackend(root string) (*LocalCanvasBackend, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve canvas root: %w", err)
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, fmt.Errorf("create canvas root: %w", err)
	}
	return &LocalCanvasBackend{root: abs}, nil
}

func (b *LocalCanvasBackend) Name() string { return "local" }
func (b *LocalCanvasBackend) Close() error { return nil }

func (b *LocalCanvasBackend) Create(_ context.Context, sessionID, name, content string) (*CanvasInfo, error) {
	if err := b.validateSessionID(sessionID); err != nil {
		return nil, err
	}
	dir := b.canvasDir(sessionID, name)
	if _, err := os.Stat(dir); err == nil {
		return nil, domain.NewSubSystemError("canvas", "CanvasBackend.Create", domain.ErrDuplicate,
			fmt.Sprintf("canvas %q already exists", name))
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create canvas dir: %w", err)
	}

	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write canvas file: %w", err)
	}

	now := time.Now()
	info := &CanvasInfo{
		Name:      name,
		SessionID: sessionID,
		Size:      len(content),
		CreatedAt: now,
		UpdatedAt: now,
		Path:      htmlPath,
	}
	b.writeMeta(dir, info)
	return info, nil
}

func (b *LocalCanvasBackend) Update(_ context.Context, sessionID, name, content string) (*CanvasInfo, error) {
	if err := b.validateSessionID(sessionID); err != nil {
		return nil, err
	}
	dir := b.canvasDir(sessionID, name)
	if fi, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, domain.NewSubSystemError("canvas", "CanvasBackend.Update", domain.ErrNotFound,
				fmt.Sprintf("canvas %q not found", name))
		}
		return nil, fmt.Errorf("stat canvas dir: %w", err)
	} else if !fi.IsDir() {
		return nil, domain.NewSubSystemError("canvas", "CanvasBackend.Update", domain.ErrNotFound,
			fmt.Sprintf("canvas %q not found", name))
	}

	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write canvas file: %w", err)
	}

	meta := b.readMeta(dir)
	now := time.Now()
	info := &CanvasInfo{
		Name:      name,
		SessionID: sessionID,
		Size:      len(content),
		CreatedAt: meta.CreatedAt,
		UpdatedAt: now,
		Path:      htmlPath,
	}
	if info.CreatedAt.IsZero() {
		info.CreatedAt = now
	}
	b.writeMeta(dir, info)
	return info, nil
}

func (b *LocalCanvasBackend) Read(_ context.Context, sessionID, name string) (*CanvasContent, error) {
	if err := b.validateSessionID(sessionID); err != nil {
		return nil, err
	}
	dir := b.canvasDir(sessionID, name)
	htmlPath := filepath.Join(dir, "index.html")

	data, err := os.ReadFile(htmlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.NewSubSystemError("canvas", "CanvasBackend.Read", domain.ErrNotFound,
				fmt.Sprintf("canvas %q not found", name))
		}
		return nil, fmt.Errorf("read canvas: %w", err)
	}

	meta := b.readMeta(dir)
	return &CanvasContent{
		CanvasInfo: CanvasInfo{
			Name:      name,
			SessionID: sessionID,
			Size:      len(data),
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Path:      htmlPath,
		},
		Content: string(data),
	}, nil
}

func (b *LocalCanvasBackend) Delete(_ context.Context, sessionID, name string) error {
	if err := b.validateSessionID(sessionID); err != nil {
		return err
	}
	dir := b.canvasDir(sessionID, name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return domain.NewSubSystemError("canvas", "CanvasBackend.Delete", domain.ErrNotFound,
			fmt.Sprintf("canvas %q not found", name))
	}
	return os.RemoveAll(dir)
}

func (b *LocalCanvasBackend) List(_ context.Context, sessionID string) ([]CanvasInfo, error) {
	if err := b.validateSessionID(sessionID); err != nil {
		return nil, err
	}
	sessionDir := filepath.Join(b.root, sessionID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list canvases: %w", err)
	}

	var canvases []CanvasInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(sessionDir, entry.Name())
		htmlPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(htmlPath); err != nil {
			continue
		}
		meta := b.readMeta(dir)
		if meta.Name == "" {
			meta.Name = entry.Name()
		}
		meta.SessionID = sessionID
		meta.Path = htmlPath
		canvases = append(canvases, *meta)
	}
	return canvases, nil
}

func (b *LocalCanvasBackend) canvasDir(sessionID, name string) string {
	return filepath.Join(b.root, sessionID, name)
}

func (b *LocalCanvasBackend) metaPath(dir string) string {
	return filepath.Join(dir, ".canvas-meta.json")
}

func (b *LocalCanvasBackend) writeMeta(dir string, info *CanvasInfo) {
	data, _ := json.Marshal(info)
	_ = os.WriteFile(b.metaPath(dir), data, 0600)
}

func (b *LocalCanvasBackend) readMeta(dir string) *CanvasInfo {
	data, err := os.ReadFile(b.metaPath(dir))
	if err != nil {
		return &CanvasInfo{}
	}
	var info CanvasInfo
	_ = json.Unmarshal(data, &info)
	return &info
}

func (b *LocalCanvasBackend) validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}
	if strings.ContainsAny(sessionID, `/\.`) || strings.Contains(sessionID, "..") {
		return fmt.Errorf("invalid session ID %q: contains unsafe characters", sessionID)
	}
	if len(sessionID) > 128 {
		return fmt.Errorf("session ID too long: %d chars (max 128)", len(sessionID))
	}
	return nil
}
