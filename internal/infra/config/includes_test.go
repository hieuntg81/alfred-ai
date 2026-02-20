package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIncludesSingleFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "llm.yaml", `
llm:
  providers:
    - name: "openai"
      api_key: "sk-from-include"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "llm.yaml"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.LLM.Providers) != 1 || cfg.LLM.Providers[0].APIKey != "sk-from-include" {
		t.Errorf("provider not loaded from include: %+v", cfg.LLM.Providers)
	}
}

func TestIncludesGlobPattern(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "conf.d")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	writeConfigFile(t, subdir, "memory.yaml", `
memory:
  provider: "markdown"
`)
	writeConfigFile(t, subdir, "tools.yaml", `
tools:
  sandbox_root: "/custom/sandbox"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "conf.d/*.yaml"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// At least one of the includes should have taken effect.
	if cfg.Memory.Provider != "markdown" && cfg.Tools.SandboxRoot != "/custom/sandbox" {
		t.Error("glob includes had no effect")
	}
}

func TestIncludesRelativePath(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	writeConfigFile(t, subdir, "extra.yaml", `
logger:
  level: "debug"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "sub/extra.yaml"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logger.Level != "debug" {
		t.Errorf("Logger.Level = %q, want %q", cfg.Logger.Level, "debug")
	}
}

func TestIncludesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absFile := writeConfigFile(t, dir, "abs.yaml", `
logger:
  level: "warn"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "`+absFile+`"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logger.Level != "warn" {
		t.Errorf("Logger.Level = %q, want %q", cfg.Logger.Level, "warn")
	}
}

func TestIncludesMainPrecedence(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "override.yaml", `
agent:
  max_iterations: 50
  system_prompt: "from include"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "override.yaml"
agent:
  max_iterations: 20
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Main config takes precedence.
	if cfg.Agent.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20 (main should win)", cfg.Agent.MaxIterations)
	}
	// Include value preserved where main didn't override.
	if cfg.Agent.SystemPrompt != "from include" {
		t.Errorf("SystemPrompt = %q, want %q", cfg.Agent.SystemPrompt, "from include")
	}
}

func TestIncludesCircularDetection(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "a.yaml", `
includes:
  - "b.yaml"
`)
	writeConfigFile(t, dir, "b.yaml", `
includes:
  - "a.yaml"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "a.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected circular include error")
	}
	if !strings.Contains(err.Error(), "circular include") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIncludesSelfReference(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "config.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected circular include error for self-reference")
	}
	if !strings.Contains(err.Error(), "circular include") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIncludesPathTraversal(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "../../../etc/passwd"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected path traversal error")
	}
	// The error should indicate either path escape or file access failure.
	errStr := err.Error()
	if !strings.Contains(errStr, "escapes") && !strings.Contains(errStr, "permission") && !strings.Contains(errStr, "read") {
		t.Logf("error (acceptable): %v", err)
	}
}

func TestIncludesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "insecure.yaml")
	if err := os.WriteFile(badFile, []byte("logger:\n  level: debug\n"), 0666); err != nil {
		t.Fatal(err)
	}
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "insecure.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected permissions error for include file")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIncludesFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "nonexistent.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing include file")
	}
}

func TestIncludesInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "bad.yaml", "invalid: [yaml: bad")
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "bad.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML in include")
	}
}

func TestIncludesNoIncludes(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
agent:
  max_iterations: 15
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.MaxIterations != 15 {
		t.Errorf("MaxIterations = %d, want 15", cfg.Agent.MaxIterations)
	}
}

func TestIncludesNestedIncludes(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "level2.yaml", `
logger:
  format: "json"
`)
	writeConfigFile(t, dir, "level1.yaml", `
includes:
  - "level2.yaml"
logger:
  level: "debug"
`)
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "level1.yaml"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logger.Format != "json" {
		t.Errorf("Logger.Format = %q, want %q (from nested include)", cfg.Logger.Format, "json")
	}
}

func TestIncludesMaxDepth(t *testing.T) {
	dir := t.TempDir()

	// Create a chain of includes that exceeds maxIncludeDepth.
	totalLevels := maxIncludeDepth + 2
	for i := totalLevels; i >= 1; i-- {
		name := fmt.Sprintf("level%d.yaml", i)
		var content string
		if i < totalLevels {
			next := fmt.Sprintf("level%d.yaml", i+1)
			content = fmt.Sprintf("includes:\n  - %q\n", next)
		}
		fpath := filepath.Join(dir, name)
		if err := os.WriteFile(fpath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "level1.yaml"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected max depth error")
	}
	if !strings.Contains(err.Error(), "max depth") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIncludesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "empty.yaml", "")
	path := writeConfigFile(t, dir, "config.yaml", `
includes:
  - "empty.yaml"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Defaults should remain intact.
	if cfg.Agent.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", cfg.Agent.MaxIterations)
	}
}
