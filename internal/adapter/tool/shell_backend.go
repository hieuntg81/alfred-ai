package tool

import "context"

// ShellBackend abstracts command execution.
type ShellBackend interface {
	// Execute runs a command and returns stdout, stderr, and any error.
	Execute(ctx context.Context, command string, args []string, workDir string) (stdout, stderr string, err error)
	// Name returns the backend identifier (e.g. "local").
	Name() string
}
