package tool

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// LocalShellBackend executes commands on the local system.
type LocalShellBackend struct {
	timeout time.Duration
}

// NewLocalShellBackend creates a local shell backend with the given command timeout.
func NewLocalShellBackend(timeout time.Duration) *LocalShellBackend {
	return &LocalShellBackend{timeout: timeout}
}

func (b *LocalShellBackend) Name() string { return "local" }

func (b *LocalShellBackend) Execute(ctx context.Context, command string, args []string, workDir string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
