package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"alfred-ai/internal/infra/config"
)

// CheckStatus represents the result of a health check.
type CheckStatus string

const (
	StatusPass CheckStatus = "PASS"
	StatusWarn CheckStatus = "WARN"
	StatusFail CheckStatus = "FAIL"
)

// CheckResult holds the outcome of a single health check.
type CheckResult struct {
	Name    string
	Status  CheckStatus
	Message string
	Fix     string // optional fix suggestion
}

// Check is a named health check function.
type Check struct {
	Name string
	Fn   func(cfg *config.Config) CheckResult
}

// runDoctor executes all health checks and reports results.
func runDoctor() error {
	cfgPath := configPath()

	// Try to load config — some checks work without it.
	cfg, cfgErr := config.Load(cfgPath)

	checks := []Check{
		{Name: "Config file", Fn: checkConfigFile(cfgPath, cfgErr)},
		{Name: "LLM API key", Fn: checkLLMAPIKey},
		{Name: "LLM connectivity", Fn: checkLLMConnectivity},
		{Name: "Memory backend", Fn: checkMemoryBackend},
		{Name: "Channel config", Fn: checkChannelConfig},
		{Name: "Tool dependencies", Fn: checkToolDependencies},
		{Name: "Disk space", Fn: checkDiskSpace},
		{Name: "Network", Fn: checkNetwork},
		{Name: "Chromium", Fn: checkChromium},
		{Name: "SearXNG", Fn: checkSearXNG},
	}

	fmt.Println("alfred-ai doctor")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	var pass, warn, fail int
	results := make([]CheckResult, 0, len(checks))

	for _, check := range checks {
		result := check.Fn(cfg)
		result.Name = check.Name
		results = append(results, result)

		icon := statusIcon(result.Status)
		fmt.Printf("  %s %s: %s\n", icon, result.Name, result.Message)
		if result.Fix != "" {
			fmt.Printf("      Fix: %s\n", result.Fix)
		}

		switch result.Status {
		case StatusPass:
			pass++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Results: %d passed, %d warnings, %d failed\n", pass, warn, fail)

	if fail > 0 {
		fmt.Println("\nFix the FAIL issues above to ensure alfred-ai runs correctly.")
		return fmt.Errorf("%d check(s) failed", fail)
	}
	if warn > 0 {
		fmt.Println("\nalfred-ai should work, but consider addressing the warnings.")
	} else {
		fmt.Println("\nAll checks passed! alfred-ai is ready to run.")
	}
	return nil
}

func statusIcon(s CheckStatus) string {
	switch s {
	case StatusPass:
		return "[PASS]"
	case StatusWarn:
		return "[WARN]"
	case StatusFail:
		return "[FAIL]"
	default:
		return "[????]"
	}
}

// checkConfigFile returns a check that verifies the config file exists and parses correctly.
func checkConfigFile(cfgPath string, cfgErr error) func(*config.Config) CheckResult {
	return func(_ *config.Config) CheckResult {
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			return CheckResult{
				Status:  StatusFail,
				Message: fmt.Sprintf("config file not found at %s", cfgPath),
				Fix:     "Run 'alfred-ai setup' to create a config file",
			}
		}

		if cfgErr != nil {
			return CheckResult{
				Status:  StatusFail,
				Message: fmt.Sprintf("config file parse error: %v", cfgErr),
				Fix:     "Check config.yaml syntax; run 'alfred-ai setup' to regenerate",
			}
		}

		return CheckResult{
			Status:  StatusPass,
			Message: fmt.Sprintf("config loaded from %s", cfgPath),
		}
	}
}

// checkLLMAPIKey verifies at least one LLM provider has an API key configured.
func checkLLMAPIKey(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot check — config not loaded",
		}
	}

	if len(cfg.LLM.Providers) == 0 {
		return CheckResult{
			Status:  StatusFail,
			Message: "no LLM providers configured",
			Fix:     "Add at least one provider in config.yaml under llm.providers",
		}
	}

	var withKey, withoutKey []string
	for _, p := range cfg.LLM.Providers {
		if p.APIKey != "" {
			withKey = append(withKey, p.Name)
		} else {
			withoutKey = append(withoutKey, p.Name)
		}
	}

	if len(withKey) == 0 {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("no API keys found for providers: %s", strings.Join(withoutKey, ", ")),
			Fix:     "Set API keys via environment variables (e.g., ALFREDAI_LLM_PROVIDER_OPENAI_API_KEY)",
		}
	}

	if len(withoutKey) > 0 {
		return CheckResult{
			Status:  StatusWarn,
			Message: fmt.Sprintf("keys configured for [%s]; missing for [%s]", strings.Join(withKey, ", "), strings.Join(withoutKey, ", ")),
		}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("API keys configured for: %s", strings.Join(withKey, ", ")),
	}
}

// checkLLMConnectivity tests if the default LLM provider is reachable.
func checkLLMConnectivity(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot check — config not loaded",
		}
	}

	// Find the default provider config.
	var provider *config.ProviderConfig
	for i := range cfg.LLM.Providers {
		if cfg.LLM.Providers[i].Name == cfg.LLM.DefaultProvider {
			provider = &cfg.LLM.Providers[i]
			break
		}
	}
	if provider == nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("default provider %q not found in config", cfg.LLM.DefaultProvider),
		}
	}

	if provider.APIKey == "" {
		return CheckResult{
			Status:  StatusWarn,
			Message: "skipped — no API key for default provider",
		}
	}

	// Determine the API endpoint to check.
	endpoint := providerEndpoint(provider)
	if endpoint == "" {
		return CheckResult{
			Status:  StatusWarn,
			Message: fmt.Sprintf("no known endpoint for provider type %q — skipping connectivity test", provider.Type),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("failed to create request: %v", err),
		}
	}

	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("cannot reach %s: %v", endpoint, err),
			Fix:     "Check your internet connection and firewall settings",
		}
	}
	resp.Body.Close()

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("%s reachable (latency: %dms)", provider.Name, latency.Milliseconds()),
	}
}

// providerEndpoint returns a health/ping URL for the given provider.
func providerEndpoint(p *config.ProviderConfig) string {
	switch p.Type {
	case "openai", "":
		if p.BaseURL != "" {
			return strings.TrimRight(p.BaseURL, "/")
		}
		return "https://api.openai.com/v1/models"
	case "anthropic":
		if p.BaseURL != "" {
			return strings.TrimRight(p.BaseURL, "/")
		}
		return "https://api.anthropic.com/"
	case "gemini":
		if p.BaseURL != "" {
			return strings.TrimRight(p.BaseURL, "/")
		}
		return "https://generativelanguage.googleapis.com/"
	case "openrouter":
		if p.BaseURL != "" {
			return strings.TrimRight(p.BaseURL, "/")
		}
		return "https://openrouter.ai/api/v1/models"
	case "ollama":
		baseURL := "http://localhost:11434"
		if p.BaseURL != "" {
			baseURL = strings.TrimRight(p.BaseURL, "/")
		}
		return baseURL + "/api/tags"
	default:
		if p.BaseURL != "" {
			return strings.TrimRight(p.BaseURL, "/")
		}
		return ""
	}
}

// checkMemoryBackend verifies the memory data directory exists and is writable.
func checkMemoryBackend(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot check — config not loaded",
		}
	}

	provider := cfg.Memory.Provider
	if provider == "noop" || provider == "" {
		return CheckResult{
			Status:  StatusPass,
			Message: "memory provider is noop (no persistence)",
		}
	}

	dataDir := cfg.Memory.DataDir
	if dataDir == "" {
		dataDir = "./data/memory"
	}

	absDir, _ := filepath.Abs(dataDir)

	// Check if directory exists.
	info, err := os.Stat(absDir)
	if os.IsNotExist(err) {
		// Try to create it.
		if mkErr := os.MkdirAll(absDir, 0755); mkErr != nil {
			return CheckResult{
				Status:  StatusFail,
				Message: fmt.Sprintf("data directory %s does not exist and cannot be created: %v", absDir, mkErr),
				Fix:     fmt.Sprintf("Create the directory: mkdir -p %s", absDir),
			}
		}
		return CheckResult{
			Status:  StatusPass,
			Message: fmt.Sprintf("data directory created at %s (provider: %s)", absDir, provider),
		}
	}
	if err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("cannot stat data directory: %v", err),
		}
	}
	if !info.IsDir() {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("%s exists but is not a directory", absDir),
		}
	}

	// Check writability by creating a temp file.
	testFile := filepath.Join(absDir, ".doctor-check")
	if err := os.WriteFile(testFile, []byte("ok"), 0644); err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("data directory %s is not writable: %v", absDir, err),
			Fix:     fmt.Sprintf("Fix permissions: chmod 755 %s", absDir),
		}
	}
	os.Remove(testFile)

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("data directory %s writable (provider: %s)", absDir, provider),
	}
}

// checkChannelConfig verifies at least one channel is configured.
func checkChannelConfig(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot check — config not loaded",
		}
	}

	if len(cfg.Channels) == 0 {
		return CheckResult{
			Status:  StatusWarn,
			Message: "no channels configured — will default to CLI",
		}
	}

	types := make([]string, len(cfg.Channels))
	for i, ch := range cfg.Channels {
		types[i] = ch.Type
	}
	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("%d channel(s): %s", len(cfg.Channels), strings.Join(types, ", ")),
	}
}

// checkToolDependencies checks for optional tool dependencies.
func checkToolDependencies(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "cannot check — config not loaded",
		}
	}

	var missing []string

	if cfg.Tools.BrowserEnabled {
		if _, err := exec.LookPath("chromium"); err != nil {
			if _, err := exec.LookPath("chromium-browser"); err != nil {
				if _, err := exec.LookPath("google-chrome"); err != nil {
					missing = append(missing, "chromium (needed for browser tool)")
				}
			}
		}
	}

	if cfg.Tools.ShellBackend == "local" {
		if _, err := exec.LookPath("bash"); err != nil {
			missing = append(missing, "bash (needed for shell tool)")
		}
	}

	if len(missing) > 0 {
		return CheckResult{
			Status:  StatusWarn,
			Message: fmt.Sprintf("missing optional dependencies: %s", strings.Join(missing, "; ")),
			Fix:     "Install the missing tools or disable the features that need them",
		}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: "all required tool dependencies found",
	}
}

// checkDiskSpace checks available disk space in the data directory.
func checkDiskSpace(cfg *config.Config) CheckResult {
	dataDir := "./data"
	if cfg != nil && cfg.Memory.DataDir != "" {
		dataDir = cfg.Memory.DataDir
	}

	absDir, _ := filepath.Abs(dataDir)

	// Use du to check current usage if directory exists.
	info, err := os.Stat(absDir)
	if err != nil || !info.IsDir() {
		return CheckResult{
			Status:  StatusPass,
			Message: "data directory does not exist yet — space check skipped",
		}
	}

	// Try to get disk usage with df.
	out, err := exec.Command("df", "-h", absDir).Output()
	if err != nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "could not determine disk space (df command failed)",
		}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return CheckResult{
			Status:  StatusWarn,
			Message: "unexpected df output format",
		}
	}

	// Parse the last line (the actual disk info).
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 5 {
		return CheckResult{
			Status:  StatusWarn,
			Message: "unexpected df output format",
		}
	}

	available := fields[3]
	usePercent := fields[4]

	// Parse usage percentage.
	pctStr := strings.TrimSuffix(usePercent, "%")
	var pct int
	fmt.Sscanf(pctStr, "%d", &pct)

	if pct >= 95 {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("disk almost full: %s used, %s available", usePercent, available),
			Fix:     "Free up disk space or change data_dir to a different partition",
		}
	}
	if pct >= 85 {
		return CheckResult{
			Status:  StatusWarn,
			Message: fmt.Sprintf("disk usage high: %s used, %s available", usePercent, available),
		}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("disk usage: %s used, %s available", usePercent, available),
	}
}

// checkNetwork verifies basic internet connectivity.
func checkNetwork(_ *config.Config) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "1.1.1.1:443")
	if err != nil {
		// Try Google DNS as fallback.
		conn2, err2 := d.DialContext(ctx, "tcp", "8.8.8.8:443")
		if err2 != nil {
			return CheckResult{
				Status:  StatusFail,
				Message: "no internet connectivity detected",
				Fix:     "Check your network connection and firewall settings",
			}
		}
		conn2.Close()
	} else {
		conn.Close()
	}

	return CheckResult{
		Status:  StatusPass,
		Message: "internet connectivity OK",
	}
}

// checkChromium checks if a Chromium-compatible browser is installed.
func checkChromium(cfg *config.Config) CheckResult {
	if cfg != nil && !cfg.Tools.BrowserEnabled {
		return CheckResult{
			Status:  StatusPass,
			Message: "browser tool disabled — Chromium not required",
		}
	}

	browsers := []string{"chromium", "chromium-browser", "google-chrome"}
	for _, name := range browsers {
		path, err := exec.LookPath(name)
		if err == nil {
			return CheckResult{
				Status:  StatusPass,
				Message: fmt.Sprintf("found %s at %s", name, path),
			}
		}
	}

	status := StatusWarn
	msg := "Chromium not found"
	fix := "Install Chromium: apt install chromium (or set tools.browser_enabled: false)"

	if cfg != nil && cfg.Tools.BrowserEnabled {
		status = StatusFail
		msg = "Chromium not found but browser tool is enabled"
	}

	return CheckResult{
		Status:  status,
		Message: msg,
		Fix:     fix,
	}
}

// checkSearXNG checks if SearXNG is running for web search.
func checkSearXNG(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "cannot check — config not loaded",
		}
	}

	if cfg.Tools.SearchBackend != "searxng" {
		return CheckResult{
			Status:  StatusPass,
			Message: fmt.Sprintf("search backend is %q — SearXNG not required", cfg.Tools.SearchBackend),
		}
	}

	url := cfg.Tools.SearXNGURL
	if url == "" {
		url = "http://localhost:8888"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("invalid SearXNG URL: %v", err),
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("SearXNG not reachable at %s: %v", url, err),
			Fix:     "Start SearXNG: docker compose up -d searxng (or update tools.searxng_url)",
		}
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return CheckResult{
			Status:  StatusWarn,
			Message: fmt.Sprintf("SearXNG responded with status %d at %s", resp.StatusCode, url),
		}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("SearXNG reachable at %s", url),
	}
}
