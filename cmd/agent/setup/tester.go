package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"alfred-ai/internal/adapter/channel"
	"alfred-ai/internal/infra/config"
	"gopkg.in/yaml.v3"
)

// TestResult represents the result of a configuration test.
type TestResult struct {
	Step    string
	Success bool
	Message string
	Error   error
}

// TestConfiguration tests a configuration by launching alfred-ai and simulating user interaction.
func TestConfiguration(cfg *config.Config, ui *UIHelper) ([]TestResult, error) {
	var results []TestResult

	// Step 1: Save test config to temporary file with isolated data dir
	ui.PrintInfo("⏳", "Preparing test environment...")
	testConfigPath, testDataDir, err := saveTestConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to save test config: %w", err)
	}
	defer os.Remove(testConfigPath) // Clean up test config
	defer os.RemoveAll(testDataDir) // Clean up test data directory

	results = append(results, TestResult{
		Step:    "Config Validation",
		Success: true,
		Message: "Configuration file is valid",
	})

	// Step 2: Launch test instance
	ui.PrintInfo("⏳", "Launching alfred-ai for testing...")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	instance, err := launchTestInstance(ctx, testConfigPath)
	if err != nil {
		results = append(results, TestResult{
			Step:    "App Launch",
			Success: false,
			Message: "Failed to launch alfred-ai",
			Error:   err,
		})
		return results, err
	}
	defer instance.Shutdown()

	results = append(results, TestResult{
		Step:    "App Launch",
		Success: true,
		Message: "alfred-ai started successfully",
	})

	// Step 3: Wait for bot to be ready
	ui.PrintInfo("⏳", "Waiting for bot to initialize...")
	if err := instance.WaitForReady(10 * time.Second); err != nil {
		results = append(results, TestResult{
			Step:    "Initialization",
			Success: false,
			Message: "Bot did not initialize in time",
			Error:   err,
		})
		return results, err
	}

	results = append(results, TestResult{
		Step:    "Initialization",
		Success: true,
		Message: "Bot is ready to receive messages",
	})

	// Step 4: Test conversation
	ui.PrintInfo("⏳", "Testing AI conversation...")
	ui.PrintEmptyLine()

	// Test message 1: Introduction
	testMsg1 := "Hello! Can you introduce yourself in one sentence?"
	ui.PrintConversation("user", testMsg1)

	resp1, err := instance.SendAndWait(testMsg1, 30*time.Second)
	if err != nil {
		results = append(results, TestResult{
			Step:    "AI Response",
			Success: false,
			Message: "Failed to get response from AI",
			Error:   err,
		})
		return results, err
	}

	ui.PrintConversation("assistant", resp1)

	if len(resp1) < 10 {
		results = append(results, TestResult{
			Step:    "AI Response",
			Success: false,
			Message: "Response too short, possible error",
		})
		return results, fmt.Errorf("invalid response from AI")
	}

	results = append(results, TestResult{
		Step:    "AI Response",
		Success: true,
		Message: "Bot responds to messages correctly",
	})

	// Step 5: Test memory (if enabled)
	if cfg.Memory.Provider != "noop" {
		ui.PrintInfo("⏳", "Testing memory persistence...")
		ui.PrintEmptyLine()

		testMsg2 := "Please remember that my favorite color is blue."
		ui.PrintConversation("user", testMsg2)

		resp2, err := instance.SendAndWait(testMsg2, 30*time.Second)
		if err == nil {
			ui.PrintConversation("assistant", resp2)

			// Wait for memory curator to finish saving
			time.Sleep(2 * time.Second)

			// Clear session so model must rely on memory system, not conversation context
			instance.SendCommand("/clear")
			time.Sleep(500 * time.Millisecond)

			// Test recall from memory (not conversation history)
			testMsg3 := "What is my favorite color?"
			ui.PrintConversation("user", testMsg3)

			resp3, err := instance.SendAndWait(testMsg3, 30*time.Second)
			if err == nil {
				ui.PrintConversation("assistant", resp3)

				// Check if response mentions "blue"
				if strings.Contains(strings.ToLower(resp3), "blue") {
					results = append(results, TestResult{
						Step:    "Memory System",
						Success: true,
						Message: "Memory save and recall working",
					})
				} else {
					results = append(results, TestResult{
						Step:    "Memory System",
						Success: false,
						Message: "Memory recall did not work as expected",
					})
				}
			}
		}

		if err != nil {
			results = append(results, TestResult{
				Step:    "Memory System",
				Success: false,
				Message: "Memory test encountered an error",
				Error:   err,
			})
		}
	}

	ui.PrintEmptyLine()
	return results, nil
}

// lineResult holds a single line read from the subprocess stdout.
type lineResult struct {
	line string
	err  error
}

// TestInstance represents a running alfred-ai instance for testing.
type TestInstance struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bufio.Reader
	lines  chan lineResult
}

// launchTestInstance starts alfred-ai in a subprocess.
func launchTestInstance(ctx context.Context, configPath string) (*TestInstance, error) {
	// Find the alfred-ai binary
	binaryPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to find alfred-ai binary: %w", err)
	}

	// Launch with test config
	cmd := exec.CommandContext(ctx, binaryPath, "--config="+configPath)

	// Enable structured response markers for reliable parsing
	cmd.Env = append(os.Environ(), channel.EnvCLIResponseMarkers+"=1")

	// Set up pipes
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start alfred-ai: %w", err)
	}

	inst := &TestInstance{
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: bufio.NewReader(stdoutPipe),
		stderr: bufio.NewReader(stderrPipe),
		lines:  make(chan lineResult, 64),
	}

	// Single persistent goroutine to read lines — avoids concurrent
	// access to the bufio.Reader which is not thread-safe.
	go func() {
		for {
			line, err := inst.stdout.ReadString('\n')
			inst.lines <- lineResult{line, err}
			if err != nil {
				return
			}
		}
	}()

	return inst, nil
}

// WaitForReady waits for the bot to be ready to receive messages.
func (t *TestInstance) WaitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Read initial output until we see the prompt or timeout
	for time.Now().Before(deadline) {
		// Try to read with short timeout
		line, err := t.readLineWithTimeout(1 * time.Second)
		if err != nil && err != io.EOF {
			continue
		}

		// Look for indicators that the bot is ready
		if strings.Contains(line, ">") || strings.Contains(line, "ready") || strings.Contains(line, "started") {
			return nil
		}
	}

	return fmt.Errorf("bot did not become ready within timeout")
}

// SendCommand sends a CLI command (e.g. /clear) without waiting for response markers.
func (t *TestInstance) SendCommand(cmd string) error {
	_, err := t.stdin.Write([]byte(cmd + "\n"))
	return err
}

// SendAndWait sends a message and waits for a response.
// It uses structured response markers (<<ALFRED_RESPONSE>> / <<END_RESPONSE>>)
// to reliably extract model responses from stdout, ignoring all CLI UI noise.
func (t *TestInstance) SendAndWait(message string, timeout time.Duration) (string, error) {
	// Send message
	if _, err := t.stdin.Write([]byte(message + "\n")); err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	deadline := time.Now().Add(timeout)

	// Phase 1: Read lines until we find the start marker.
	for time.Now().Before(deadline) {
		line, err := t.readLineWithTimeout(1 * time.Second)
		if err != nil {
			if err == io.EOF {
				return "", fmt.Errorf("EOF before response marker")
			}
			continue
		}

		if strings.Contains(line, channel.ResponseStartMarker) {
			break
		}
	}

	if !time.Now().Before(deadline) {
		return "", fmt.Errorf("no response received within timeout (waiting for start marker)")
	}

	// Phase 2: Collect lines until we find the end marker.
	var response strings.Builder
	for time.Now().Before(deadline) {
		line, err := t.readLineWithTimeout(1 * time.Second)
		if err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if strings.TrimSpace(line) == channel.ResponseEndMarker {
			result := strings.TrimSpace(response.String())
			if result == "" {
				return "", fmt.Errorf("empty response between markers")
			}
			return result, nil
		}

		response.WriteString(line)
	}

	// If we collected some content but never saw end marker, still return it
	if response.Len() > 0 {
		return strings.TrimSpace(response.String()), nil
	}

	return "", fmt.Errorf("no response received within timeout (waiting for end marker)")
}

// readLineWithTimeout reads a line with a timeout.
// It reads from the persistent lines channel fed by a single reader goroutine,
// avoiding concurrent access to the underlying bufio.Reader.
func (t *TestInstance) readLineWithTimeout(timeout time.Duration) (string, error) {
	select {
	case res := <-t.lines:
		return res.line, res.err
	case <-time.After(timeout):
		return "", fmt.Errorf("read timeout")
	}
}

// Shutdown gracefully shuts down the test instance.
func (t *TestInstance) Shutdown() error {
	// Try to send quit command
	if t.stdin != nil {
		t.stdin.Write([]byte("/quit\n"))
		t.stdin.Close()
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		// Force kill if it doesn't exit gracefully
		if t.cmd.Process != nil {
			t.cmd.Process.Kill()
		}
		return nil
	}
}

// saveTestConfig saves a config to a temporary file for testing.
// Returns the config file path, the temporary data directory path, and any error.
// Both are cleaned up by the caller.
func saveTestConfig(cfg *config.Config) (configPath string, dataDir string, err error) {
	// Create a unique temp directory per test run to isolate session state
	dataDir = filepath.Join(os.TempDir(), fmt.Sprintf("alfred-ai-test-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create test data directory: %w", err)
	}

	// Config file also goes to a temp location
	configPath = filepath.Join(os.TempDir(), fmt.Sprintf("alfred-ai-test-config-%d.yaml", time.Now().UnixNano()))

	cfg.Memory.DataDir = filepath.Join(dataDir, "memory")
	cfg.Security.Audit.Path = filepath.Join(dataDir, "audit.jsonl")

	// Append test instruction to system prompt so the model answers concisely
	cfg.Agent.SystemPrompt += "\n\nDuring testing: answer questions directly and concisely. When asked to remember something, confirm briefly. When asked to recall, answer with just the fact."

	// Marshal config to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, dataDir, nil
}
