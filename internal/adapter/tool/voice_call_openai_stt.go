package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"nhooyr.io/websocket"
)

// OpenAISTTConfig holds configuration for the OpenAI STT provider.
type OpenAISTTConfig struct {
	APIKey          string
	Model           string // "gpt-4o-transcribe"
	BaseURL         string // WebSocket URL for OpenAI Realtime API
	SilenceDurationMs int  // silence threshold for VAD
}

// OpenAISTTProvider implements STTProvider using the OpenAI Realtime API.
type OpenAISTTProvider struct {
	config OpenAISTTConfig
	logger *slog.Logger
}

// NewOpenAISTTProvider creates a new OpenAI STT provider.
func NewOpenAISTTProvider(cfg OpenAISTTConfig, logger *slog.Logger) *OpenAISTTProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "wss://api.openai.com/v1/realtime"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-transcribe"
	}
	if cfg.SilenceDurationMs <= 0 {
		cfg.SilenceDurationMs = 800
	}
	return &OpenAISTTProvider{
		config: cfg,
		logger: logger,
	}
}

func (p *OpenAISTTProvider) Name() string { return "openai-stt" }

// StartSession opens a WebSocket connection to the OpenAI Realtime API.
func (p *OpenAISTTProvider) StartSession(ctx context.Context, cfg STTSessionConfig) (STTSession, error) {
	wsURL := fmt.Sprintf("%s?model=%s", p.config.BaseURL, p.config.Model)

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Authorization": {"Bearer " + p.config.APIKey},
			"OpenAI-Beta":   {"realtime=v1"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stt websocket connect: %w", err)
	}

	session := &openAISTTSession{
		conn:        conn,
		transcripts: make(chan STTTranscript, 32),
		done:        make(chan struct{}),
		logger:      p.logger,
	}

	// Send session configuration.
	sessionCfg := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"input_audio_format":      cfg.Encoding,
			"input_audio_transcription": map[string]any{
				"model": cfg.Model,
			},
			"turn_detection": map[string]any{
				"type":                "server_vad",
				"silence_duration_ms": p.config.SilenceDurationMs,
			},
		},
	}

	cfgData, err := json.Marshal(sessionCfg)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "config marshal error")
		return nil, fmt.Errorf("marshal session config: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, cfgData); err != nil {
		conn.Close(websocket.StatusInternalError, "config write error")
		return nil, fmt.Errorf("send session config: %w", err)
	}

	// Start reading responses.
	go session.readLoop()

	return session, nil
}

// openAISTTSession is an active STT session connected to the OpenAI Realtime API.
type openAISTTSession struct {
	conn        *websocket.Conn
	transcripts chan STTTranscript
	done        chan struct{}
	closeOnce   sync.Once
	logger      *slog.Logger
}

func (s *openAISTTSession) SendAudio(data []byte) error {
	select {
	case <-s.done:
		return fmt.Errorf("stt session closed")
	default:
	}

	// Send audio as input_audio_buffer.append event.
	msg := map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": data, // base64 encoding handled by JSON marshal
	}

	msgData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal audio message: %w", err)
	}

	ctx := context.Background()
	if err := s.conn.Write(ctx, websocket.MessageText, msgData); err != nil {
		return fmt.Errorf("send audio: %w", err)
	}

	return nil
}

func (s *openAISTTSession) Transcripts() <-chan STTTranscript {
	return s.transcripts
}

func (s *openAISTTSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.conn.Close(websocket.StatusNormalClosure, "session ended")
	})
	return nil
}

// readLoop reads WebSocket messages and extracts transcription results.
func (s *openAISTTSession) readLoop() {
	defer close(s.transcripts)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		_, data, err := s.conn.Read(context.Background())
		if err != nil {
			select {
			case <-s.done:
				// Expected close.
			default:
				s.transcripts <- STTTranscript{Err: err}
			}
			return
		}

		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "conversation.item.input_audio_transcription.completed":
			var transcription struct {
				Transcript string `json:"transcript"`
			}
			if err := json.Unmarshal(data, &transcription); err == nil && transcription.Transcript != "" {
				s.transcripts <- STTTranscript{
					Text:    transcription.Transcript,
					IsFinal: true,
				}
			}

		case "conversation.item.input_audio_transcription.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal(data, &delta); err == nil && delta.Delta != "" {
				s.transcripts <- STTTranscript{
					Text:    delta.Delta,
					IsFinal: false,
				}
			}

		case "error":
			var errMsg struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(data, &errMsg); err == nil {
				s.logger.Warn("stt error", "message", errMsg.Error.Message)
				s.transcripts <- STTTranscript{Err: fmt.Errorf("stt error: %s", errMsg.Error.Message)}
			}
		}
	}
}
