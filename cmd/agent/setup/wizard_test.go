package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alfred-ai/internal/infra/config"
)

// simInput builds a string of simulated user inputs (one per line).
func simInput(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestWizardFullRun(t *testing.T) {
	input := simInput(
		"1",           // LLM: OpenAI
		"sk-test-key", // API key
		"1",           // Model: first in list (gpt-4o-mini)
		"1",           // Memory: noop
		"n",           // Encryption: no
		"1",           // Channels: CLI
		"",            // System prompt: default
	)
	var output bytes.Buffer
	wiz := NewWizard(strings.NewReader(input), &output)

	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cfg.LLM.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q", cfg.LLM.DefaultProvider)
	}
	if len(cfg.LLM.Providers) != 1 || cfg.LLM.Providers[0].APIKey != "sk-test-key" {
		t.Errorf("Provider: %+v", cfg.LLM.Providers)
	}
	if cfg.Memory.Provider != "noop" {
		t.Errorf("Memory.Provider = %q", cfg.Memory.Provider)
	}
	if cfg.Security.Encryption.Enabled {
		t.Error("encryption should be disabled")
	}
	if !strings.Contains(output.String(), "Setup complete") {
		t.Error("missing setup complete message")
	}
}

func TestWizardOpenAIProvider(t *testing.T) {
	input := simInput("1", "sk-openai", "2", "1", "n", "1", "")
	//                  ^provider  ^key  ^model(gpt-4o) ^mem ^enc ^ch ^prompt
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cfg.LLM.Providers[0].Type != "openai" || cfg.LLM.Providers[0].Model != "gpt-4o" {
		t.Errorf("provider: %+v", cfg.LLM.Providers[0])
	}
}

func TestWizardAnthropicProvider(t *testing.T) {
	input := simInput("2", "sk-anthropic", "1", "1", "n", "1", "")
	//                  ^provider    ^key  ^model(claude-sonnet-4-5) ^mem ^enc ^ch ^prompt
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cfg.LLM.Providers[0].Type != "anthropic" {
		t.Errorf("type = %q", cfg.LLM.Providers[0].Type)
	}
	if cfg.LLM.Providers[0].Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q", cfg.LLM.Providers[0].Model)
	}
}

func TestWizardGeminiProvider(t *testing.T) {
	input := simInput("3", "gemini-key", "1", "1", "n", "1", "")
	//                  ^provider ^key  ^model(gemini-2.5-flash) ^mem ^enc ^ch ^prompt
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cfg.LLM.Providers[0].Type != "gemini" {
		t.Errorf("type = %q", cfg.LLM.Providers[0].Type)
	}
	if cfg.LLM.Providers[0].Model != "gemini-2.5-flash" {
		t.Errorf("model = %q", cfg.LLM.Providers[0].Model)
	}
}

func TestWizardCustomProvider(t *testing.T) {
	input := simInput(
		"4",                          // Custom
		"my-provider",                // name
		"openai",                     // type
		"my-model",                   // model
		"https://api.example.com/v1", // base URL
		"sk-custom",                  // API key
		"1",                          // memory
		"n",                          // encryption
		"1",                          // channel
		"",                           // system prompt
	)
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	p := cfg.LLM.Providers[0]
	if p.Name != "my-provider" || p.Model != "my-model" || p.BaseURL != "https://api.example.com/v1" {
		t.Errorf("provider: %+v", p)
	}
}

func TestWizardMemoryProviders(t *testing.T) {
	for _, tc := range []struct {
		choice   string
		expected string
	}{
		{"1", "noop"},
		{"2", "markdown"},
		{"3", "vector"},
	} {
		input := simInput("1", "key", "1", tc.choice, "n", "1", "")
		//                  ^prov ^key ^model ^mem    ^enc ^ch  ^prompt
		wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
		cfg, err := wiz.Run()
		if err != nil {
			t.Fatalf("Run (%s): %v", tc.expected, err)
		}
		if cfg.Memory.Provider != tc.expected {
			t.Errorf("Memory.Provider = %q, want %q", cfg.Memory.Provider, tc.expected)
		}
	}
}

func TestWizardMemoryByteRover(t *testing.T) {
	input := simInput(
		"1", "key", "1", // LLM + model
		"4",                                   // byterover
		"https://api.byterover.com", "br-key", // byterover config
		"n", "1", "", // enc, channels, prompt
	)
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cfg.Memory.Provider != "byterover" {
		t.Errorf("Provider = %q", cfg.Memory.Provider)
	}
	if cfg.Memory.ByteRover.BaseURL != "https://api.byterover.com" {
		t.Errorf("BaseURL = %q", cfg.Memory.ByteRover.BaseURL)
	}
}

func TestWizardEncryptionYes(t *testing.T) {
	input := simInput("1", "key", "1", "1", "y", "1", "")
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !cfg.Security.Encryption.Enabled {
		t.Error("encryption should be enabled")
	}
}

func TestWizardChannelsCLI(t *testing.T) {
	input := simInput("1", "key", "1", "1", "n", "1", "")
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Type != "cli" {
		t.Errorf("channels: %+v", cfg.Channels)
	}
}

func TestWizardChannelsMultiple(t *testing.T) {
	input := simInput(
		"1", "key", "1", "1", "n",
		"1,2",   // CLI + HTTP
		":9090", // HTTP addr
		"",      // system prompt
	)
	wiz := NewWizard(strings.NewReader(input), &bytes.Buffer{})
	cfg, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(cfg.Channels))
	}
	if cfg.Channels[0].Type != "cli" {
		t.Errorf("channels[0].type = %q", cfg.Channels[0].Type)
	}
	if cfg.Channels[1].Type != "http" || cfg.Channels[1].HTTP == nil || cfg.Channels[1].HTTP.Addr != ":9090" {
		t.Errorf("channels[1] = %+v", cfg.Channels[1])
	}
}

func TestWizardAskStringDefault(t *testing.T) {
	wiz := NewWizard(strings.NewReader("\n"), &bytes.Buffer{})
	val, err := wiz.askString("test", "default-val")
	if err != nil {
		t.Fatalf("askString: %v", err)
	}
	if val != "default-val" {
		t.Errorf("val = %q, want %q", val, "default-val")
	}
}

func TestWizardAskYesNoDefault(t *testing.T) {
	wiz := NewWizard(strings.NewReader("\n"), &bytes.Buffer{})
	val, err := wiz.askYesNo("test", true)
	if err != nil {
		t.Fatalf("askYesNo: %v", err)
	}
	if !val {
		t.Error("expected true (default)")
	}
}

func TestWizardAskChoiceInvalid(t *testing.T) {
	// First invalid, then valid.
	input := simInput("invalid", "2")
	var output bytes.Buffer
	wiz := NewWizard(strings.NewReader(input), &output)
	choice, err := wiz.askChoice(3)
	if err != nil {
		t.Fatalf("askChoice: %v", err)
	}
	if choice != 2 {
		t.Errorf("choice = %d, want 2", choice)
	}
	if !strings.Contains(output.String(), "Please enter") {
		t.Error("expected retry message")
	}
}

func TestWizardAskChoiceOutOfRange(t *testing.T) {
	// First out of range, then valid.
	input := simInput("5", "1")
	var output bytes.Buffer
	wiz := NewWizard(strings.NewReader(input), &output)
	choice, err := wiz.askChoice(3)
	if err != nil {
		t.Fatalf("askChoice: %v", err)
	}
	if choice != 1 {
		t.Errorf("choice = %d, want 1", choice)
	}
}

func TestWizardHeaderPrinted(t *testing.T) {
	input := simInput("1", "key", "1", "1", "n", "1", "")
	var output bytes.Buffer
	wiz := NewWizard(strings.NewReader(input), &output)
	_, err := wiz.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output.String(), "alfred-ai setup") {
		t.Error("expected header")
	}
}

func TestSaveConfig(t *testing.T) {
	cfg := config.Defaults()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	var output bytes.Buffer

	if err := SaveConfig(cfg, path, &output); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("config file is empty")
	}
	if !strings.Contains(output.String(), "Config written") {
		t.Error("expected confirmation message")
	}
}

func TestSaveConfigPermissions(t *testing.T) {
	cfg := config.Defaults()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SaveConfig(cfg, path, &bytes.Buffer{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestSaveConfigValidationError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.SystemPrompt = "" // invalid
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := SaveConfig(cfg, path, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Errorf("unexpected error: %v", err)
	}
}
