package tool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// OpenAITTSConfig holds configuration for the OpenAI TTS provider.
type OpenAITTSConfig struct {
	APIKey     string
	Model      string // "tts-1" or "tts-1-hd"
	Voice      string // "alloy", "echo", "fable", "onyx", "nova", "shimmer"
	BaseURL    string // defaults to "https://api.openai.com"
}

// OpenAITTSProvider implements TTSProvider using the OpenAI TTS API.
type OpenAITTSProvider struct {
	config OpenAITTSConfig
	client *http.Client
	logger *slog.Logger
}

// NewOpenAITTSProvider creates a new OpenAI TTS provider.
func NewOpenAITTSProvider(cfg OpenAITTSConfig, logger *slog.Logger) *OpenAITTSProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	if cfg.Model == "" {
		cfg.Model = "tts-1"
	}
	if cfg.Voice == "" {
		cfg.Voice = "alloy"
	}
	return &OpenAITTSProvider{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

func (p *OpenAITTSProvider) Name() string { return "openai-tts" }

// SynthesizeStream sends a TTS request to OpenAI and streams PCM audio chunks.
func (p *OpenAITTSProvider) SynthesizeStream(ctx context.Context, req TTSSynthesizeRequest) (<-chan TTSAudioChunk, error) {
	voice := req.Voice
	if voice == "" {
		voice = p.config.Voice
	}
	model := req.Model
	if model == "" {
		model = p.config.Model
	}

	apiURL := p.config.BaseURL + "/v1/audio/speech"
	bodyStr := fmt.Sprintf(
		`{"model":%q,"input":%q,"voice":%q,"response_format":"pcm"}`,
		model, req.Text, voice,
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(bodyStr))
	if err != nil {
		return nil, fmt.Errorf("create tts request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tts api call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("tts api error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Stream response body in chunks.
	ch := make(chan TTSAudioChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case ch <- TTSAudioChunk{PCMData: data}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					select {
					case ch <- TTSAudioChunk{Err: err}:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()

	return ch, nil
}
