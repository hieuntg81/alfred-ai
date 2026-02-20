package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"alfred-ai/internal/domain"
)

func TestSandboxValidPath(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	resolved, err := sandbox.ValidatePath(testFile)
	if err != nil {
		t.Errorf("valid path should pass: %v", err)
	}
	if resolved != testFile {
		t.Errorf("resolved = %q, want %q", resolved, testFile)
	}
}

func TestSandboxPathTraversal(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		filepath.Join(dir, "..", "etc", "passwd"),
		"/etc/passwd",
		filepath.Join(dir, "..", "..", "root", ".ssh"),
	}

	for _, path := range tests {
		_, err := sandbox.ValidatePath(path)
		if !errors.Is(err, domain.ErrPathOutsideSandbox) {
			t.Errorf("path %q: expected ErrPathOutsideSandbox, got %v", path, err)
		}
	}
}

func TestSandboxNewFilePath(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Path to a file that doesn't exist yet (but parent does)
	newFile := filepath.Join(dir, "newfile.txt")
	resolved, err := sandbox.ValidatePath(newFile)
	if err != nil {
		t.Errorf("new file in sandbox should pass: %v", err)
	}
	if resolved != newFile {
		t.Errorf("resolved = %q, want %q", resolved, newFile)
	}
}

func TestSandboxSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a symlink pointing outside the sandbox
	outsideDir := t.TempDir()
	symlink := filepath.Join(dir, "escape")
	if err := os.Symlink(outsideDir, symlink); err != nil {
		t.Skip("cannot create symlinks")
	}

	_, err = sandbox.ValidatePath(filepath.Join(symlink, "file.txt"))
	if !errors.Is(err, domain.ErrPathOutsideSandbox) {
		t.Errorf("symlink escape: expected ErrPathOutsideSandbox, got %v", err)
	}
}

func TestSandboxRoot(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	root := sandbox.Root()
	if root == "" {
		t.Error("Root() returned empty string")
	}
	if root != dir {
		t.Errorf("Root() = %q, want %q", root, dir)
	}
}

func TestNewSandboxNonExistentPath(t *testing.T) {
	_, err := NewSandbox("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestNewSandboxNotDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir.txt")
	if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := NewSandbox(file)
	if err == nil {
		t.Error("expected error for regular file")
	}
}

func TestSandboxRootItself(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := sandbox.ValidatePath(dir)
	if err != nil {
		t.Errorf("root path should be valid: %v", err)
	}
	if resolved != dir {
		t.Errorf("resolved = %q, want %q", resolved, dir)
	}
}

func TestSandboxValidatePathParentNotExist(t *testing.T) {
	dir := t.TempDir()
	sandbox, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Path where BOTH the file AND the parent directory do not exist
	// This triggers the EvalSymlinks error on the parent (line 53-54 in sandbox.go)
	deepPath := filepath.Join(dir, "nonexistent_parent", "nonexistent_child.txt")
	_, err = sandbox.ValidatePath(deepPath)
	if err == nil {
		t.Error("expected error for path with nonexistent parent")
	}
	if !errors.Is(err, domain.ErrPathOutsideSandbox) {
		t.Errorf("expected ErrPathOutsideSandbox, got %v", err)
	}
}

func TestNewSandboxSymlinkRoot(t *testing.T) {
	// Test NewSandbox with a symlink pointing to a valid directory
	// This exercises the EvalSymlinks success path in NewSandbox
	dir := t.TempDir()
	symlinkDir := t.TempDir()
	symlink := filepath.Join(symlinkDir, "link_to_dir")
	if err := os.Symlink(dir, symlink); err != nil {
		t.Skip("cannot create symlinks")
	}

	sandbox, err := NewSandbox(symlink)
	if err != nil {
		t.Fatalf("NewSandbox with symlink: %v", err)
	}

	// Root should be the resolved (real) path, not the symlink
	if sandbox.Root() != dir {
		t.Errorf("Root() = %q, want %q", sandbox.Root(), dir)
	}
}
