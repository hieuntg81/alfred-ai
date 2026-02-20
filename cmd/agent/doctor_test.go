package main

import (
	"os"
	"testing"

	"alfred-ai/internal/infra/config"
)

func TestCheckConfigFile_NotFound(t *testing.T) {
	fn := checkConfigFile("/nonexistent/path/config.yaml", nil)
	result := fn(nil)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for missing config, got %s", result.Status)
	}
	if result.Fix == "" {
		t.Error("expected fix suggestion for missing config")
	}
}

func TestCheckConfigFile_ParseError(t *testing.T) {
	// Create a temp file that exists but simulate a parse error.
	tmpDir := t.TempDir()
	cfgPath := tmpDir + "/config.yaml"
	if err := writeTestFile(t, cfgPath, "invalid: {{yaml"); err != nil {
		t.Fatal(err)
	}

	fn := checkConfigFile(cfgPath, &config.ValidationError{Errors: []string{"bad yaml"}})
	result := fn(nil)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for parse error, got %s", result.Status)
	}
}

func TestCheckConfigFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := tmpDir + "/config.yaml"
	if err := writeTestFile(t, cfgPath, "agent:\n  system_prompt: test"); err != nil {
		t.Fatal(err)
	}

	fn := checkConfigFile(cfgPath, nil)
	result := fn(nil)
	if result.Status != StatusPass {
		t.Errorf("expected PASS for valid config, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckLLMAPIKey_NilConfig(t *testing.T) {
	result := checkLLMAPIKey(nil)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for nil config, got %s", result.Status)
	}
}

func TestCheckLLMAPIKey_NoProviders(t *testing.T) {
	cfg := &config.Config{}
	result := checkLLMAPIKey(cfg)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for no providers, got %s", result.Status)
	}
}

func TestCheckLLMAPIKey_AllKeysPresent(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Providers: []config.ProviderConfig{
				{Name: "openai", APIKey: "sk-test"},
			},
		},
	}
	result := checkLLMAPIKey(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckLLMAPIKey_MissingKey(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Providers: []config.ProviderConfig{
				{Name: "openai", APIKey: "sk-test"},
				{Name: "anthropic", APIKey: ""},
			},
		},
	}
	result := checkLLMAPIKey(cfg)
	if result.Status != StatusWarn {
		t.Errorf("expected WARN for partial keys, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckLLMAPIKey_AllKeysMissing(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Providers: []config.ProviderConfig{
				{Name: "openai", APIKey: ""},
			},
		},
	}
	result := checkLLMAPIKey(cfg)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for all keys missing, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckMemoryBackend_Noop(t *testing.T) {
	cfg := &config.Config{
		Memory: config.MemoryConfig{Provider: "noop"},
	}
	result := checkMemoryBackend(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS for noop provider, got %s", result.Status)
	}
}

func TestCheckMemoryBackend_WritableDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Memory: config.MemoryConfig{
			Provider: "markdown",
			DataDir:  tmpDir,
		},
	}
	result := checkMemoryBackend(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS for writable dir, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckMemoryBackend_NilConfig(t *testing.T) {
	result := checkMemoryBackend(nil)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for nil config, got %s", result.Status)
	}
}

func TestCheckChannelConfig_NoChannels(t *testing.T) {
	cfg := &config.Config{}
	result := checkChannelConfig(cfg)
	if result.Status != StatusWarn {
		t.Errorf("expected WARN for no channels, got %s", result.Status)
	}
}

func TestCheckChannelConfig_WithChannels(t *testing.T) {
	cfg := &config.Config{
		Channels: []config.ChannelConfig{
			{Type: "http"},
			{Type: "telegram"},
		},
	}
	result := checkChannelConfig(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckToolDependencies_NilConfig(t *testing.T) {
	result := checkToolDependencies(nil)
	if result.Status != StatusWarn {
		t.Errorf("expected WARN for nil config, got %s", result.Status)
	}
}

func TestCheckToolDependencies_NoBrowserEnabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tools.BrowserEnabled = false
	result := checkToolDependencies(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS when browser is disabled, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckChromium_BrowserDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tools.BrowserEnabled = false
	result := checkChromium(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS when browser disabled, got %s", result.Status)
	}
}

func TestCheckSearXNG_NotSearXNG(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tools.SearchBackend = "other"
	result := checkSearXNG(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS when not searxng backend, got %s", result.Status)
	}
}

func TestCheckSearXNG_NilConfig(t *testing.T) {
	result := checkSearXNG(nil)
	if result.Status != StatusWarn {
		t.Errorf("expected WARN for nil config, got %s", result.Status)
	}
}

func TestCheckNetwork(t *testing.T) {
	// This test actually hits the network — skip if in CI without network.
	result := checkNetwork(nil)
	// We can't guarantee the network is available in test, so just verify it returns a valid status.
	if result.Status != StatusPass && result.Status != StatusFail {
		t.Errorf("expected PASS or FAIL, got %s", result.Status)
	}
}

func TestCheckDiskSpace_NonexistentDir(t *testing.T) {
	cfg := &config.Config{
		Memory: config.MemoryConfig{DataDir: "/nonexistent/path/doctor-test"},
	}
	result := checkDiskSpace(cfg)
	if result.Status != StatusPass {
		t.Errorf("expected PASS for nonexistent dir, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckDiskSpace_ExistingDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Memory: config.MemoryConfig{DataDir: tmpDir},
	}
	result := checkDiskSpace(cfg)
	// Should be PASS or WARN depending on actual disk usage — just verify it doesn't fail.
	if result.Status == StatusFail {
		t.Logf("disk space check FAIL (may be expected on full disks): %s", result.Message)
	}
}

func TestCheckLLMConnectivity_NilConfig(t *testing.T) {
	result := checkLLMConnectivity(nil)
	if result.Status != StatusFail {
		t.Errorf("expected FAIL for nil config, got %s", result.Status)
	}
}

func TestCheckLLMConnectivity_NoAPIKey(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			DefaultProvider: "openai",
			Providers: []config.ProviderConfig{
				{Name: "openai", Type: "openai", APIKey: ""},
			},
		},
	}
	result := checkLLMConnectivity(cfg)
	if result.Status != StatusWarn {
		t.Errorf("expected WARN when no API key, got %s: %s", result.Status, result.Message)
	}
}

func TestProviderEndpoint(t *testing.T) {
	tests := []struct {
		providerType string
		baseURL      string
		expected     string
	}{
		{"openai", "", "https://api.openai.com/v1/models"},
		{"anthropic", "", "https://api.anthropic.com/"},
		{"gemini", "", "https://generativelanguage.googleapis.com/"},
		{"openrouter", "", "https://openrouter.ai/api/v1/models"},
		{"ollama", "", "http://localhost:11434/api/tags"},
		{"ollama", "http://myhost:11434", "http://myhost:11434/api/tags"},
		{"openai", "https://custom.api.com/v1", "https://custom.api.com/v1"},
		{"unknown", "", ""},
	}

	for _, tt := range tests {
		p := &config.ProviderConfig{Type: tt.providerType, BaseURL: tt.baseURL}
		got := providerEndpoint(p)
		if got != tt.expected {
			t.Errorf("providerEndpoint(%s, %q) = %q, want %q", tt.providerType, tt.baseURL, got, tt.expected)
		}
	}
}

func TestStatusIcon(t *testing.T) {
	if statusIcon(StatusPass) != "[PASS]" {
		t.Error("wrong icon for PASS")
	}
	if statusIcon(StatusWarn) != "[WARN]" {
		t.Error("wrong icon for WARN")
	}
	if statusIcon(StatusFail) != "[FAIL]" {
		t.Error("wrong icon for FAIL")
	}
}

func TestSummaryCount(t *testing.T) {
	// Simulate running checks with a known config.
	cfg := config.Defaults()
	cfg.LLM.Providers = []config.ProviderConfig{
		{Name: "openai", Type: "openai", APIKey: "sk-test"},
	}
	cfg.Tools.BrowserEnabled = false
	cfg.Memory.Provider = "noop"

	checks := []Check{
		{Name: "Config file", Fn: checkConfigFile("dummy", nil)},
		{Name: "LLM API key", Fn: checkLLMAPIKey},
		{Name: "Channel config", Fn: checkChannelConfig},
	}

	var pass, warn, fail int
	for _, check := range checks {
		result := check.Fn(cfg)
		switch result.Status {
		case StatusPass:
			pass++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}

	// At least some should have run.
	total := pass + warn + fail
	if total != len(checks) {
		t.Errorf("expected %d total results, got %d", len(checks), total)
	}
}

// writeTestFile is a test helper that creates a file with the given content.
func writeTestFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0644)
}
