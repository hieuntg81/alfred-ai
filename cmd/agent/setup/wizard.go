package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"alfred-ai/internal/infra/config"
)

// Wizard guides the user through first-time configuration.
type Wizard struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewWizard creates a Wizard with the given I/O.
func NewWizard(r io.Reader, w io.Writer) *Wizard {
	return &Wizard{
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// Run executes the interactive setup wizard and returns a Config.
func (w *Wizard) Run() (*config.Config, error) {
	cfg := config.Defaults()

	w.printHeader()

	// Step 1: LLM Provider
	provider, err := w.chooseLLMProvider()
	if err != nil {
		return nil, err
	}
	cfg.LLM.DefaultProvider = provider.Name
	cfg.LLM.Providers = []config.ProviderConfig{provider}

	// Step 2: Memory Provider
	memProvider, err := w.chooseMemoryProvider()
	if err != nil {
		return nil, err
	}
	cfg.Memory.Provider = memProvider

	if memProvider == "byterover" {
		baseURL, err := w.askString("ByteRover base URL", "https://api.byterover.com")
		if err != nil {
			return nil, err
		}
		apiKey, err := w.askSecret("ByteRover API key")
		if err != nil {
			return nil, err
		}
		cfg.Memory.ByteRover.BaseURL = baseURL
		cfg.Memory.ByteRover.APIKey = apiKey
	}

	// Step 3: Encryption
	enableEnc, err := w.askYesNo("Enable config encryption?", false)
	if err != nil {
		return nil, err
	}
	cfg.Security.Encryption.Enabled = enableEnc

	// Step 4: Channels
	channels, err := w.chooseChannels()
	if err != nil {
		return nil, err
	}
	cfg.Channels = channels

	// Step 5: System Prompt
	prompt, err := w.askString("System prompt", cfg.Agent.SystemPrompt)
	if err != nil {
		return nil, err
	}
	cfg.Agent.SystemPrompt = prompt

	fmt.Fprintln(w.writer)
	fmt.Fprintln(w.writer, "Setup complete!")
	return cfg, nil
}

func (w *Wizard) printHeader() {
	fmt.Fprintln(w.writer, "=== alfred-ai setup ===")
	fmt.Fprintln(w.writer, "This wizard will create your initial configuration.")
	fmt.Fprintln(w.writer)
}

func (w *Wizard) chooseLLMProvider() (config.ProviderConfig, error) {
	fmt.Fprintln(w.writer, "Select LLM provider:")
	fmt.Fprintln(w.writer, "  1) OpenAI")
	fmt.Fprintln(w.writer, "  2) Anthropic")
	fmt.Fprintln(w.writer, "  3) Gemini")
	fmt.Fprintln(w.writer, "  4) Custom")

	choice, err := w.askChoice(4)
	if err != nil {
		return config.ProviderConfig{}, err
	}

	var p config.ProviderConfig
	switch choice {
	case 1:
		p = config.ProviderConfig{Name: "openai", Type: "openai"}
	case 2:
		p = config.ProviderConfig{Name: "anthropic", Type: "anthropic"}
	case 3:
		p = config.ProviderConfig{Name: "gemini", Type: "gemini"}
	case 4:
		name, err := w.askString("Provider name", "custom")
		if err != nil {
			return config.ProviderConfig{}, err
		}
		pType, err := w.askString("Provider type (openai/anthropic/gemini)", "openai")
		if err != nil {
			return config.ProviderConfig{}, err
		}
		model, err := w.askString("Model name", "")
		if err != nil {
			return config.ProviderConfig{}, err
		}
		baseURL, err := w.askString("Base URL (leave empty for default)", "")
		if err != nil {
			return config.ProviderConfig{}, err
		}
		p = config.ProviderConfig{Name: name, Type: pType, Model: model, BaseURL: baseURL}
	}

	apiKey, err := w.askSecret("API key (or set via env var later)")
	if err != nil {
		return config.ProviderConfig{}, err
	}
	p.APIKey = apiKey

	// Model selection for standard providers (custom already has model set above)
	if p.Model == "" {
		model, err := w.chooseModel(p.Type, p.APIKey)
		if err != nil {
			return config.ProviderConfig{}, err
		}
		p.Model = model
	}

	return p, nil
}

// chooseModel fetches available models and lets the user pick one.
func (w *Wizard) chooseModel(providerType, apiKey string) (string, error) {
	fmt.Fprintln(w.writer)
	fmt.Fprintln(w.writer, "Fetching available models...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models := GetModelsWithFallback(ctx, providerType, apiKey)
	recommended := RecommendedModel(providerType)

	if len(models) == 0 {
		if recommended != "" {
			return recommended, nil
		}
		return "default", nil
	}

	fmt.Fprintln(w.writer, "Select model:")
	for i, m := range models {
		marker := ""
		if m.ID == recommended {
			marker = " (recommended)"
		}
		if m.Description != "" {
			fmt.Fprintf(w.writer, "  %d) %s%s - %s\n", i+1, m.ID, marker, m.Description)
		} else {
			fmt.Fprintf(w.writer, "  %d) %s%s\n", i+1, m.ID, marker)
		}
	}

	choice, err := w.askChoice(len(models))
	if err != nil {
		return "", err
	}

	return models[choice-1].ID, nil
}

func (w *Wizard) chooseMemoryProvider() (string, error) {
	fmt.Fprintln(w.writer, "Select memory provider:")
	fmt.Fprintln(w.writer, "  1) noop (no persistent memory)")
	fmt.Fprintln(w.writer, "  2) markdown (local files)")
	fmt.Fprintln(w.writer, "  3) vector (semantic search)")
	fmt.Fprintln(w.writer, "  4) byterover (cloud)")

	choice, err := w.askChoice(4)
	if err != nil {
		return "", err
	}

	switch choice {
	case 1:
		return "noop", nil
	case 2:
		return "markdown", nil
	case 3:
		return "vector", nil
	case 4:
		return "byterover", nil
	}
	return "noop", nil
}

func (w *Wizard) chooseChannels() ([]config.ChannelConfig, error) {
	fmt.Fprintln(w.writer, "Select channels (comma-separated, e.g. 1,3):")
	fmt.Fprintln(w.writer, "  1) CLI")
	fmt.Fprintln(w.writer, "  2) HTTP")
	fmt.Fprintln(w.writer, "  3) Telegram")
	fmt.Fprintln(w.writer, "  4) Discord")
	fmt.Fprintln(w.writer, "  5) Slack")

	line, err := w.askString("Channels", "1")
	if err != nil {
		return nil, err
	}

	var channels []config.ChannelConfig
	for _, part := range strings.Split(line, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		switch n {
		case 1:
			channels = append(channels, config.ChannelConfig{Type: "cli"})
		case 2:
			addr, err := w.askString("HTTP listen address", ":8080")
			if err != nil {
				return nil, err
			}
			channels = append(channels, config.ChannelConfig{Type: "http", HTTP: &config.HTTPChannelConfig{Addr: addr}})
		case 3:
			token, err := w.askSecret("Telegram bot token")
			if err != nil {
				return nil, err
			}
			channels = append(channels, config.ChannelConfig{Type: "telegram", Telegram: &config.TelegramChannelConfig{Token: token}})
		case 4:
			token, err := w.askSecret("Discord bot token")
			if err != nil {
				return nil, err
			}
			channels = append(channels, config.ChannelConfig{Type: "discord", Discord: &config.DiscordChannelConfig{Token: token}})
		case 5:
			botTok, err := w.askSecret("Slack bot token")
			if err != nil {
				return nil, err
			}
			appTok, err := w.askSecret("Slack app token")
			if err != nil {
				return nil, err
			}
			channels = append(channels, config.ChannelConfig{Type: "slack", Slack: &config.SlackChannelConfig{BotToken: botTok, AppToken: appTok}})
		}
	}

	if len(channels) == 0 {
		channels = []config.ChannelConfig{{Type: "cli"}}
	}
	return channels, nil
}

func (w *Wizard) askString(prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(w.writer, "%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Fprintf(w.writer, "%s: ", prompt)
	}
	line, err := w.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

func (w *Wizard) askSecret(prompt string) (string, error) {
	fmt.Fprintf(w.writer, "%s: ", prompt)
	line, err := w.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (w *Wizard) askYesNo(prompt string, defaultVal bool) (bool, error) {
	def := "y/N"
	if defaultVal {
		def = "Y/n"
	}
	fmt.Fprintf(w.writer, "%s [%s]: ", prompt, def)
	line, err := w.reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultVal, nil
	}
	return line == "y" || line == "yes", nil
}

func (w *Wizard) askChoice(max int) (int, error) {
	for {
		fmt.Fprintf(w.writer, "Choice [1-%d]: ", max)
		line, err := w.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || n < 1 || n > max {
			fmt.Fprintf(w.writer, "Please enter a number between 1 and %d.\n", max)
			continue
		}
		return n, nil
	}
}
