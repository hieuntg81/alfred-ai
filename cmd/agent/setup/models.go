package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ModelInfo describes an available model for a provider.
type ModelInfo struct {
	ID          string
	Description string
}

// defaultModels provides static fallback lists for each provider.
var defaultModels = map[string][]ModelInfo{
	"openai": {
		{ID: "gpt-4o-mini", Description: "Fast & affordable"},
		{ID: "gpt-4o", Description: "Most capable, vision support"},
		{ID: "gpt-4-turbo", Description: "Previous gen flagship"},
		{ID: "o3-mini", Description: "Reasoning model"},
	},
	"anthropic": {
		{ID: "claude-sonnet-4-5", Description: "Best balance of speed & intelligence"},
		{ID: "claude-opus-4", Description: "Most capable"},
		{ID: "claude-haiku-3-5", Description: "Fastest & cheapest"},
	},
	"gemini": {
		{ID: "gemini-2.5-flash", Description: "Latest, fast & affordable"},
		{ID: "gemini-2.5-pro", Description: "Most capable"},
		{ID: "gemini-2.0-flash", Description: "Previous gen fast model"},
	},
	"openrouter": {
		{ID: "openai/gpt-4o-mini", Description: "OpenAI GPT-4o Mini via OpenRouter"},
		{ID: "openai/gpt-4o", Description: "OpenAI GPT-4o via OpenRouter"},
		{ID: "anthropic/claude-sonnet-4-5", Description: "Claude Sonnet 4.5 via OpenRouter"},
		{ID: "google/gemini-2.5-flash", Description: "Gemini 2.5 Flash via OpenRouter"},
	},
}

// recommendedModels maps provider type to the recommended default model.
var recommendedModels = map[string]string{
	"openai":     "gpt-4o-mini",
	"anthropic":  "claude-sonnet-4-5",
	"gemini":     "gemini-2.5-flash",
	"openrouter": "openai/gpt-4o-mini",
}

// RecommendedModel returns the recommended default model for a provider.
func RecommendedModel(providerType string) string {
	if m, ok := recommendedModels[providerType]; ok {
		return m
	}
	return ""
}

// FetchModels fetches available models from a provider's API.
// Returns an error if the provider doesn't support model listing or the API call fails.
func FetchModels(ctx context.Context, providerType, apiKey string) ([]ModelInfo, error) {
	switch providerType {
	case "openai":
		return fetchOpenAIModels(ctx, apiKey)
	case "gemini":
		return fetchGeminiModels(ctx, apiKey)
	case "anthropic":
		return nil, fmt.Errorf("Anthropic does not support model listing API")
	case "openrouter":
		return fetchOpenRouterModels(ctx, apiKey)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// GetModelsWithFallback fetches models from the API, falling back to the static list on failure.
func GetModelsWithFallback(ctx context.Context, providerType, apiKey string) []ModelInfo {
	models, err := FetchModels(ctx, providerType, apiKey)
	if err == nil && len(models) > 0 {
		return models
	}

	if static, ok := defaultModels[providerType]; ok {
		return static
	}

	return nil
}

// --- OpenAI model listing ---

type openAIModelsResponse struct {
	Data []openAIModel `json:"data"`
}

type openAIModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// openAIChatPrefixes defines prefixes that indicate a chat-capable model.
var openAIChatPrefixes = []string{"gpt-", "o1-", "o3-", "o4-"}

func fetchOpenAIModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Data {
		if isOpenAIChatModel(m.ID) {
			models = append(models, ModelInfo{ID: m.ID})
		}
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

func isOpenAIChatModel(id string) bool {
	for _, prefix := range openAIChatPrefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// --- Gemini model listing ---

type geminiModelsResponse struct {
	Models []geminiModelEntry `json:"models"`
}

type geminiModelEntry struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

func fetchGeminiModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result geminiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Models {
		if !supportsGenerateContent(m.SupportedGenerationMethods) {
			continue
		}
		// Name comes as "models/gemini-1.5-flash", strip the prefix
		id := strings.TrimPrefix(m.Name, "models/")
		if !isGeminiChatModel(id) {
			continue
		}
		desc := m.DisplayName
		if desc == "" {
			desc = m.Description
		}
		models = append(models, ModelInfo{ID: id, Description: desc})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

func supportsGenerateContent(methods []string) bool {
	for _, m := range methods {
		if m == "generateContent" {
			return true
		}
	}
	return false
}

// geminiExcludeKeywords filters out non-chat Gemini models (preview, TTS, robotics, etc.)
var geminiExcludeKeywords = []string{
	"preview", "tts", "robotics", "image", "exp-", "exp1",
	"computer-use", "deep-research", "nano-banana", "-latest",
}

// isGeminiChatModel returns true if the model is a standard Gemini chat model.
func isGeminiChatModel(id string) bool {
	// Must be a Gemini model (exclude gemma, etc.)
	if !strings.HasPrefix(id, "gemini-") {
		return false
	}
	lower := strings.ToLower(id)
	for _, kw := range geminiExcludeKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}

// --- OpenRouter model listing ---

type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SupportedParameters []string `json:"supported_parameters"`
}

// openRouterPopularPrefixes defines prefixes for popular models to show in the setup wizard.
var openRouterPopularPrefixes = []string{
	"openai/gpt-4o",
	"anthropic/claude",
	"google/gemini",
	"meta-llama/llama",
}

func fetchOpenRouterModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result openRouterModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Data {
		if isOpenRouterPopularModel(m.ID) && supportsToolUse(m.SupportedParameters) {
			desc := m.Name
			if desc == "" {
				desc = m.ID
			}
			models = append(models, ModelInfo{ID: m.ID, Description: desc})
		}
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

func isOpenRouterPopularModel(id string) bool {
	for _, prefix := range openRouterPopularPrefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

func supportsToolUse(params []string) bool {
	for _, p := range params {
		if p == "tools" {
			return true
		}
	}
	return false
}
