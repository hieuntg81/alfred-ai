package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"alfred-ai/internal/domain"
)

// OfflineManager detects network connectivity and provides fallback to a local
// LLM provider when the primary (cloud) provider is unreachable.
type OfflineManager struct {
	localLLM    domain.LLMProvider
	queue       *MessageQueue
	isOnline    atomic.Bool
	checkURL    string
	checkPeriod time.Duration
	logger      *slog.Logger
}

// NewOfflineManager creates an OfflineManager.
// localLLM must be a local provider (e.g., Ollama).
func NewOfflineManager(localLLM domain.LLMProvider, queueDir, checkURL string, checkPeriod time.Duration, logger *slog.Logger) *OfflineManager {
	o := &OfflineManager{
		localLLM:    localLLM,
		queue:       NewMessageQueue(queueDir),
		checkURL:    checkURL,
		checkPeriod: checkPeriod,
		logger:      logger,
	}
	o.isOnline.Store(true) // assume online at start
	return o
}

// IsOnline returns the current connectivity status.
func (o *OfflineManager) IsOnline() bool {
	return o.isOnline.Load()
}

// HandleOffline processes a message using the local LLM when offline.
func (o *OfflineManager) HandleOffline(ctx context.Context, sessionID string, msg domain.InboundMessage) (string, error) {
	// Queue the message for later sync.
	if err := o.queue.Enqueue(QueuedMessage{
		SessionID: sessionID,
		Content:   msg.Content,
		Sender:    msg.SenderName,
		Channel:   msg.ChannelName,
		QueuedAt:  time.Now(),
	}); err != nil {
		o.logger.Warn("failed to queue message", "error", err)
	}

	// Use local LLM for immediate response.
	resp, err := o.localLLM.Chat(ctx, domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are an AI assistant running in offline mode. Some features may be limited."},
			{Role: domain.RoleUser, Content: msg.Content},
		},
	})
	if err != nil {
		return "", fmt.Errorf("offline LLM: %w", err)
	}
	return resp.Message.Content, nil
}

// Sync flushes queued messages. Called when connectivity is restored.
func (o *OfflineManager) Sync(ctx context.Context) error {
	msgs, err := o.queue.Drain()
	if err != nil {
		return fmt.Errorf("drain queue: %w", err)
	}
	if len(msgs) > 0 {
		o.logger.Info("synced offline messages", "count", len(msgs))
	}
	return nil
}

// StartMonitor begins a background connectivity check loop.
func (o *OfflineManager) StartMonitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(o.checkPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wasOnline := o.isOnline.Load()
				online := o.checkConnectivity()
				o.isOnline.Store(online)

				if !wasOnline && online {
					o.logger.Info("connectivity restored")
					if err := o.Sync(ctx); err != nil {
						o.logger.Warn("sync failed after reconnect", "error", err)
					}
				} else if wasOnline && !online {
					o.logger.Warn("connectivity lost, switching to offline mode")
				}
			}
		}
	}()
}

func (o *OfflineManager) checkConnectivity() bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(o.checkURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// QueuedMessage represents a message saved while offline for later sync.
type QueuedMessage struct {
	SessionID string    `json:"session_id"`
	Content   string    `json:"content"`
	Sender    string    `json:"sender"`
	Channel   string    `json:"channel"`
	QueuedAt  time.Time `json:"queued_at"`
}

// MessageQueue is a file-based FIFO queue for offline messages.
type MessageQueue struct {
	dir string
}

// NewMessageQueue creates a message queue backed by the given directory.
func NewMessageQueue(dir string) *MessageQueue {
	return &MessageQueue{dir: dir}
}

// Enqueue writes a message to the queue.
func (q *MessageQueue) Enqueue(msg QueuedMessage) error {
	if err := os.MkdirAll(q.dir, 0700); err != nil {
		return fmt.Errorf("create queue dir: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	filename := fmt.Sprintf("%d_%s.json", time.Now().UnixNano(), msg.SessionID)
	return os.WriteFile(filepath.Join(q.dir, filename), data, 0600)
}

// Drain reads and removes all queued messages, returning them in chronological order.
func (q *MessageQueue) Drain() ([]QueuedMessage, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read queue dir: %w", err)
	}

	var msgs []QueuedMessage
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(q.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var msg QueuedMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		msgs = append(msgs, msg)
		os.Remove(path)
	}
	return msgs, nil
}

// Len returns the number of messages in the queue.
func (q *MessageQueue) Len() int {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}
