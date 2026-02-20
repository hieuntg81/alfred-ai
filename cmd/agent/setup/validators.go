package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ValidateAPIKey validates an API key for the specified provider type.
// Returns nil if valid, error with explanation if invalid.
func ValidateAPIKey(providerType, apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch providerType {
	case "openai":
		return validateOpenAIKey(ctx, apiKey)
	case "anthropic":
		return validateAnthropicKey(ctx, apiKey)
	case "gemini":
		return validateGeminiKey(ctx, apiKey)
	case "openrouter":
		return validateOpenRouterKey(ctx, apiKey)
	default:
		return fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// validateOpenAIKey validates an OpenAI API key.
func validateOpenAIKey(ctx context.Context, apiKey string) error {
	// Make a minimal API call to list models
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("connection timeout - please check your internet connection")
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil // Valid key
	case 401:
		return fmt.Errorf("invalid API key")
	case 429:
		return fmt.Errorf("rate limit exceeded - please try again in a moment")
	case 500, 502, 503, 504:
		return fmt.Errorf("OpenAI service temporarily unavailable (status %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response from OpenAI (status %d)", resp.StatusCode)
	}
}

// validateAnthropicKey validates an Anthropic API key.
func validateAnthropicKey(ctx context.Context, apiKey string) error {
	// Make a minimal messages API call with invalid parameters to check auth
	// (Valid auth will return 400 for invalid params, invalid auth returns 401)
	reqBody := strings.NewReader(`{"model":"claude-3-5-sonnet-20241022","max_tokens":1,"messages":[{"role":"user","content":"test"}]}`)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("connection timeout - please check your internet connection")
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 400: // Both indicate valid authentication
		return nil
	case 401:
		return fmt.Errorf("invalid API key")
	case 429:
		return fmt.Errorf("rate limit exceeded - please try again in a moment")
	case 500, 502, 503, 504:
		return fmt.Errorf("Anthropic service temporarily unavailable (status %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response from Anthropic (status %d)", resp.StatusCode)
	}
}

// validateGeminiKey validates a Google Gemini API key.
func validateGeminiKey(ctx context.Context, apiKey string) error {
	// Use the generateContent endpoint with minimal request
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/gemini-2.0-flash:generateContent?key=%s", apiKey)

	reqBody := strings.NewReader(`{"contents":[{"parts":[{"text":"test"}]}]}`)

	req, err := http.NewRequestWithContext(ctx, "POST", url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("connection timeout - please check your internet connection")
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil // Valid key
	case 400:
		// Check if it's an auth error or just invalid request
		var errResp struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			if strings.Contains(strings.ToLower(errResp.Error.Message), "api key") ||
				strings.Contains(strings.ToLower(errResp.Error.Message), "invalid") {
				return fmt.Errorf("invalid API key")
			}
		}
		return nil // Other 400 errors mean key is valid but request was malformed
	case 403:
		return fmt.Errorf("invalid API key or API not enabled")
	case 429:
		return fmt.Errorf("rate limit exceeded - please try again in a moment")
	case 500, 502, 503, 504:
		return fmt.Errorf("Gemini service temporarily unavailable (status %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response from Gemini (status %d)", resp.StatusCode)
	}
}

// validateOpenRouterKey validates an OpenRouter API key.
func validateOpenRouterKey(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("connection timeout - please check your internet connection")
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		return nil // Valid key
	case 401:
		return fmt.Errorf("invalid API key")
	case 429:
		return fmt.Errorf("rate limit exceeded - please try again in a moment")
	case 500, 502, 503, 504:
		return fmt.Errorf("OpenRouter service temporarily unavailable (status %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response from OpenRouter (status %d)", resp.StatusCode)
	}
}

// GetAPIKeyInstructions returns user-friendly instructions for obtaining an API key.
func GetAPIKeyInstructions(providerType string) (url string, steps []string) {
	switch providerType {
	case "openai":
		return "https://platform.openai.com/api-keys", []string{
			"Visit: https://platform.openai.com/api-keys",
			"Sign up or log in to your OpenAI account",
			"Click 'Create new secret key'",
			"Copy the key (starts with 'sk-proj-' or 'sk-')",
			"Paste it when prompted below",
		}
	case "anthropic":
		return "https://console.anthropic.com/settings/keys", []string{
			"Visit: https://console.anthropic.com/settings/keys",
			"Sign up or log in to your Anthropic account",
			"Click 'Create Key'",
			"Copy the API key (starts with 'sk-ant-')",
			"Paste it when prompted below",
		}
	case "gemini":
		return "https://aistudio.google.com/app/apikey", []string{
			"Visit: https://aistudio.google.com/app/apikey",
			"Sign in with your Google account",
			"Click 'Create API Key'",
			"Select or create a Google Cloud project",
			"Copy the generated API key",
			"Paste it when prompted below",
		}
	case "openrouter":
		return "https://openrouter.ai/settings/keys", []string{
			"Visit: https://openrouter.ai/settings/keys",
			"Sign up or log in to your OpenRouter account",
			"Click 'Create Key'",
			"Copy the API key (starts with 'sk-or-')",
			"Paste it when prompted below",
		}
	default:
		return "", nil
	}
}
