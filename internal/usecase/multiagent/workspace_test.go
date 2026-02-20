package multiagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceAgentDir(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	dir, err := ws.AgentDir("support")
	if err != nil {
		t.Fatalf("AgentDir: %v", err)
	}
	expected := filepath.Join(base, "agents", "support")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestWorkspaceSessionDir(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	dir, err := ws.SessionDir("support")
	if err != nil {
		t.Fatalf("SessionDir: %v", err)
	}
	expected := filepath.Join(base, "agents", "support", "sessions")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestWorkspaceEmptyID(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	_, err := ws.AgentDir("")
	if err == nil {
		t.Error("expected error for empty agent ID")
	}

	_, err = ws.SessionDir("")
	if err == nil {
		t.Error("expected error for empty agent ID in SessionDir")
	}
}

func TestWorkspacePathTraversal(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	bad := []string{"../escape", "foo/bar", `foo\bar`, "..", "a/../b"}
	for _, id := range bad {
		_, err := ws.AgentDir(id)
		if err == nil {
			t.Errorf("expected error for agent ID %q", id)
		}
	}
}

func TestWorkspaceIdempotent(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	dir1, err := ws.AgentDir("test")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	dir2, err := ws.AgentDir("test")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if dir1 != dir2 {
		t.Errorf("dirs differ: %q vs %q", dir1, dir2)
	}
}

func TestWorkspacePermissions(t *testing.T) {
	base := t.TempDir()
	ws := NewWorkspace(base)

	dir, err := ws.AgentDir("secure")
	if err != nil {
		t.Fatalf("AgentDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("permissions = %o, want 0700", perm)
	}
}
