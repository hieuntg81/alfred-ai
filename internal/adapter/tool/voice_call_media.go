package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
)

const (
	mediaRingBufferSize = 64 * 1024 // 64 KiB bounded ring buffer per stream
	mediaSendQueueSize  = 64        // outbound message queue
)

// MediaStreamConfig holds configuration for a media stream handler.
type MediaStreamConfig struct {
	CallID      string
	STTProvider STTProvider
	TTSProvider TTSProvider
	Store       *CallStore
	Logger      *slog.Logger
}

// MediaStreamHandler manages bidirectional WebSocket audio for a single call.
// Inbound: Twilio media stream → mu-law → STT → transcript
// Outbound: TTS → PCM → mu-law → Twilio media stream
type MediaStreamHandler struct {
	config    MediaStreamConfig
	conn      *websocket.Conn
	stt       STTSession
	sendQueue chan []byte // outbound mu-law audio chunks
	sendBuf   *RingBuffer
	done      chan struct{}
	closeOnce sync.Once
	streamSID string // Twilio stream SID
	mu        sync.Mutex
}

// NewMediaStreamHandler creates a new handler for a media stream connection.
func NewMediaStreamHandler(cfg MediaStreamConfig) *MediaStreamHandler {
	return &MediaStreamHandler{
		config:    cfg,
		sendQueue: make(chan []byte, mediaSendQueueSize),
		sendBuf:   NewRingBuffer(mediaRingBufferSize),
		done:      make(chan struct{}),
	}
}

// HandleHTTP upgrades an HTTP request to a WebSocket and handles the media stream.
// This method blocks until the stream ends.
func (h *MediaStreamHandler) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Twilio sends from various origins.
	})
	if err != nil {
		h.config.Logger.Error("websocket accept failed", "call_id", h.config.CallID, "error", err)
		return
	}
	h.conn = conn
	defer h.Close()

	// Transition to active state.
	_ = h.config.Store.Transition(h.config.CallID, CallStateActive, "")

	// Start STT session.
	if h.config.STTProvider != nil {
		sttSession, err := h.config.STTProvider.StartSession(r.Context(), STTSessionConfig{
			Model:      "gpt-4o-transcribe",
			SampleRate: 8000,
			Encoding:   "mulaw",
		})
		if err != nil {
			h.config.Logger.Error("stt session start failed", "call_id", h.config.CallID, "error", err)
		} else {
			h.stt = sttSession
			go h.sttReadLoop()
		}
	}

	// Start outbound audio sender.
	go h.sendLoop()

	// Transition to listening.
	_ = h.config.Store.Transition(h.config.CallID, CallStateListening, "")

	// Read loop — process inbound Twilio media messages.
	h.readLoop(r.Context())
}

// readLoop reads and processes Twilio media stream WebSocket messages.
func (h *MediaStreamHandler) readLoop(ctx context.Context) {
	for {
		select {
		case <-h.done:
			return
		default:
		}

		_, data, err := h.conn.Read(ctx)
		if err != nil {
			select {
			case <-h.done:
				// Expected close.
			default:
				h.config.Logger.Debug("media stream read error", "call_id", h.config.CallID, "error", err)
			}
			return
		}

		var msg twilioStreamMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Event {
		case "start":
			h.mu.Lock()
			h.streamSID = msg.StreamSID
			h.mu.Unlock()
			h.config.Logger.Info("media stream started", "call_id", h.config.CallID, "stream_sid", msg.StreamSID)

		case "media":
			// Decode base64 mu-law audio and forward to STT.
			audioData, err := base64.StdEncoding.DecodeString(msg.Media.Payload)
			if err != nil {
				continue
			}
			if h.stt != nil {
				if err := h.stt.SendAudio(audioData); err != nil {
					h.config.Logger.Debug("stt send failed", "call_id", h.config.CallID, "error", err)
				}
			}

		case "stop":
			h.config.Logger.Info("media stream stopped", "call_id", h.config.CallID)
			return

		case "mark":
			// Playback marker reached — could be used for TTS completion tracking.
			h.config.Logger.Debug("media stream mark", "call_id", h.config.CallID, "name", msg.Mark.Name)
		}
	}
}

// sendLoop writes outbound audio from the send queue to the WebSocket.
func (h *MediaStreamHandler) sendLoop() {
	for {
		select {
		case <-h.done:
			return
		case mulawData, ok := <-h.sendQueue:
			if !ok {
				return
			}

			h.mu.Lock()
			streamSID := h.streamSID
			h.mu.Unlock()

			if streamSID == "" || h.conn == nil {
				continue
			}

			// Encode as Twilio media message.
			payload := base64.StdEncoding.EncodeToString(mulawData)
			msg := twilioStreamMessage{
				Event:     "media",
				StreamSID: streamSID,
				Media: twilioMediaPayload{
					Payload: payload,
				},
			}

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			if err := h.conn.Write(context.Background(), websocket.MessageText, data); err != nil {
				h.config.Logger.Debug("media send failed", "call_id", h.config.CallID, "error", err)
				return
			}
		}
	}
}

// sttReadLoop reads transcripts from the STT session and updates the call store.
func (h *MediaStreamHandler) sttReadLoop() {
	if h.stt == nil {
		return
	}

	for transcript := range h.stt.Transcripts() {
		if transcript.Err != nil {
			h.config.Logger.Warn("stt error", "call_id", h.config.CallID, "error", transcript.Err)
			continue
		}

		if transcript.IsFinal && transcript.Text != "" {
			_ = h.config.Store.AppendTranscript(h.config.CallID, TurnEntry{
				Role: "user",
				Text: transcript.Text,
			})
			h.config.Logger.Info("user transcript",
				"call_id", h.config.CallID,
				"text", transcript.Text,
			)
		}
	}
}

// SpeakText synthesizes text to speech and sends it to the media stream.
func (h *MediaStreamHandler) SpeakText(ctx context.Context, text string, voice string) error {
	if h.config.TTSProvider == nil {
		return fmt.Errorf("no TTS provider configured")
	}

	// Clear send queue (barge-in: interrupt current TTS).
	h.clearSendQueue()

	// Transition to speaking.
	_ = h.config.Store.Transition(h.config.CallID, CallStateSpeaking, "")

	chunks, err := h.config.TTSProvider.SynthesizeStream(ctx, TTSSynthesizeRequest{
		Text:       text,
		Voice:      voice,
		SampleRate: 24000, // OpenAI TTS outputs 24kHz
	})
	if err != nil {
		return fmt.Errorf("tts synthesis: %w", err)
	}

	// Stream TTS audio to the media stream.
	go func() {
		defer func() {
			_ = h.config.Store.Transition(h.config.CallID, CallStateListening, "")
		}()

		for chunk := range chunks {
			select {
			case <-h.done:
				return
			default:
			}

			if chunk.Err != nil {
				h.config.Logger.Warn("tts chunk error", "call_id", h.config.CallID, "error", chunk.Err)
				return
			}

			// Convert PCM 24kHz → 8kHz → mu-law.
			pcm8k := Resample24kTo8k(chunk.PCMData)
			if pcm8k == nil {
				continue
			}
			mulaw := LinearBufToMulaw(pcm8k)

			select {
			case h.sendQueue <- mulaw:
			case <-h.done:
				return
			}
		}
	}()

	return nil
}

// clearSendQueue drains the outbound send queue (for barge-in).
func (h *MediaStreamHandler) clearSendQueue() {
	for {
		select {
		case <-h.sendQueue:
		default:
			return
		}
	}
}

// Close ends the media stream session.
func (h *MediaStreamHandler) Close() {
	h.closeOnce.Do(func() {
		close(h.done)
		if h.stt != nil {
			h.stt.Close()
		}
		if h.conn != nil {
			h.conn.Close(websocket.StatusNormalClosure, "stream ended")
		}
	})
}

// --- Twilio stream message types ---

type twilioStreamMessage struct {
	Event     string              `json:"event"`
	StreamSID string             `json:"streamSid,omitempty"`
	Start     *twilioStartPayload `json:"start,omitempty"`
	Media     twilioMediaPayload  `json:"media,omitempty"`
	Mark      twilioMarkPayload   `json:"mark,omitempty"`
}

type twilioStartPayload struct {
	StreamSID  string `json:"streamSid"`
	AccountSID string `json:"accountSid"`
	CallSID    string `json:"callSid"`
}

type twilioMediaPayload struct {
	Payload   string `json:"payload"` // base64-encoded mu-law audio
	Track     string `json:"track,omitempty"`
	Chunk     string `json:"chunk,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type twilioMarkPayload struct {
	Name string `json:"name"`
}
