package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alfred-ai/internal/domain"
)

// Sandbox enforces path constraints for file operations.
type Sandbox struct {
	root string // absolute, resolved workspace root
}

// NewSandbox creates a sandbox rooted at the given directory.
func NewSandbox(root string) (*Sandbox, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve sandbox root: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("eval symlinks for sandbox root: %w", err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat sandbox root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sandbox root %q is not a directory", resolved)
	}

	return &Sandbox{root: resolved}, nil
}

// ValidatePath checks that a requested path resolves to within the sandbox.
// It resolves symlinks AFTER computing the absolute path.
func (s *Sandbox) ValidatePath(requested string) (string, error) {
	abs, err := filepath.Abs(requested)
	if err != nil {
		return "", domain.NewDomainError("Sandbox.ValidatePath", domain.ErrPathOutsideSandbox, err.Error())
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Path doesn't exist yet - validate the parent directory
		parent := filepath.Dir(abs)
		resolvedParent, err2 := filepath.EvalSymlinks(parent)
		if err2 != nil {
			return "", domain.NewDomainError("Sandbox.ValidatePath", domain.ErrPathOutsideSandbox, err2.Error())
		}
		resolved = filepath.Join(resolvedParent, filepath.Base(abs))
	}

	if !s.isWithinRoot(resolved) {
		return "", domain.NewDomainError("Sandbox.ValidatePath", domain.ErrPathOutsideSandbox,
			fmt.Sprintf("resolved %q is outside root %q", resolved, s.root))
	}

	return resolved, nil
}

// Root returns the sandbox root directory.
func (s *Sandbox) Root() string { return s.root }

func (s *Sandbox) isWithinRoot(path string) bool {
	return path == s.root || strings.HasPrefix(path, s.root+string(os.PathSeparator))
}
