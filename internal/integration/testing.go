package integration

import (
	"context"
	"os"
	"testing"
	"time"
)

// Config holds integration test configuration from environment
type Config struct {
	OpenAIKey      string
	AnthropicKey   string
	GeminiKey      string
	TelegramToken  string
	TestTimeout    time.Duration
	SkipSlow       bool
}

// LoadConfig loads integration test configuration from environment
func LoadConfig() *Config {
	return &Config{
		OpenAIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicKey:  os.Getenv("ANTHROPIC_API_KEY"),
		GeminiKey:     os.Getenv("GEMINI_API_KEY"),
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TestTimeout:   60 * time.Second,
		SkipSlow:      os.Getenv("SKIP_SLOW_TESTS") == "1",
	}
}

// SkipIfNoAPIKey skips the test if the required API key is not set
func SkipIfNoAPIKey(t *testing.T, key, name string) {
	t.Helper()
	if key == "" {
		t.Skipf("Skipping %s integration test: %s_API_KEY not set", name, name)
	}
}

// SkipIfShort skips integration tests in short mode
func SkipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
}

// NewTestContext creates a context with timeout for integration tests
func NewTestContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}
