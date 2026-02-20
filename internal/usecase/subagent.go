package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// SubAgentConfig controls sub-agent spawning behavior.
type SubAgentConfig struct {
	MaxSubAgents  int
	MaxIterations int
	Timeout       time.Duration
}

// SubAgentManager spawns sub-agents for parallel task execution.
type SubAgentManager struct {
	agentFactory func() *Agent
	config       SubAgentConfig
	logger       *slog.Logger
	semaphore    chan struct{}
}

// NewSubAgentManager creates a sub-agent manager.
func NewSubAgentManager(factory func() *Agent, cfg SubAgentConfig, logger *slog.Logger) *SubAgentManager {
	if cfg.MaxSubAgents <= 0 {
		cfg.MaxSubAgents = 5
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &SubAgentManager{
		agentFactory: factory,
		config:       cfg,
		logger:       logger,
		semaphore:    make(chan struct{}, cfg.MaxSubAgents),
	}
}

// Spawn creates a fresh sub-agent session and processes a single task.
func (m *SubAgentManager) Spawn(ctx context.Context, task string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	defer cancel()

	// Acquire semaphore
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		return "", fmt.Errorf("sub-agent spawn timeout waiting for slot")
	}

	agent := m.agentFactory()
	session := NewSession(fmt.Sprintf("subagent-%d", time.Now().UnixNano()))

	m.logger.Debug("sub-agent spawned", "task", truncateString(task, 100))

	response, err := agent.HandleMessage(ctx, session, task)
	if err != nil {
		return "", fmt.Errorf("sub-agent failed: %w", err)
	}

	return response, nil
}

// SpawnParallel runs multiple tasks concurrently, respecting the semaphore limit.
func (m *SubAgentManager) SpawnParallel(ctx context.Context, tasks []string) ([]string, error) {
	results := make([]string, len(tasks))
	errors := make([]error, len(tasks))

	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t string) {
			defer wg.Done()
			result, err := m.Spawn(ctx, t)
			results[idx] = result
			errors[idx] = err
		}(i, task)
	}

	wg.Wait()

	// Collect errors
	var errs []string
	for i, err := range errors {
		if err != nil {
			errs = append(errs, fmt.Sprintf("task %d: %v", i, err))
		}
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("sub-agent errors: %s", strings.Join(errs, "; "))
	}

	return results, nil
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ErrSubAgentMaxConcurrency is returned when all sub-agent slots are in use.
var ErrSubAgentMaxConcurrency = domain.NewDomainError("SubAgentManager", fmt.Errorf("max concurrency reached"), "")
