package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

const compressSystemPrompt = `You are a conversation summarizer. Given a conversation history, produce a concise summary that preserves:
- Key facts, decisions, and conclusions
- User preferences and requirements
- Important context needed to continue the conversation
- Any pending tasks or questions

Output ONLY the summary, no preamble. Be concise but comprehensive.`

// CompressionConfig controls context compression behavior.
type CompressionConfig struct {
	Enabled    bool
	Threshold  int
	KeepRecent int
}

// Compressor summarizes old conversation messages to reduce token usage.
type Compressor struct {
	llm    domain.LLMProvider
	config CompressionConfig
	logger *slog.Logger
}

// NewCompressor creates a compressor with the given config.
func NewCompressor(llm domain.LLMProvider, cfg CompressionConfig, logger *slog.Logger) *Compressor {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 30
	}
	if cfg.KeepRecent <= 0 {
		cfg.KeepRecent = 10
	}
	return &Compressor{
		llm:    llm,
		config: cfg,
		logger: logger,
	}
}

// ShouldCompress returns true if the session has more messages than the threshold.
func (c *Compressor) ShouldCompress(session *Session) bool {
	return session.MessageCount() > c.config.Threshold
}

// Compress summarizes older messages and replaces them with a summary + recent messages.
// It only runs when the session exceeds the configured threshold.
func (c *Compressor) Compress(ctx context.Context, session *Session) error {
	if !c.ShouldCompress(session) {
		return nil
	}
	return c.compress(ctx, session)
}

// ForceCompress compresses the session regardless of threshold.
// Used by error recovery when context overflow is detected.
func (c *Compressor) ForceCompress(ctx context.Context, session *Session) error {
	return c.compress(ctx, session)
}

func (c *Compressor) compress(ctx context.Context, session *Session) error {
	msgs := session.Messages()
	if len(msgs) <= c.config.KeepRecent {
		return nil
	}

	// Messages to summarize (all except the most recent KeepRecent)
	toSummarize := msgs[:len(msgs)-c.config.KeepRecent]

	// Build conversation text for summarization
	var sb strings.Builder
	for _, msg := range toSummarize {
		if msg.Role == domain.RoleSystem {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n", msg.Role, msg.Content)
	}

	convText := sb.String()
	if strings.TrimSpace(convText) == "" {
		return nil
	}

	// Ask LLM to summarize
	req := domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: compressSystemPrompt},
			{Role: domain.RoleUser, Content: convText},
		},
		Temperature: 0.3,
	}

	resp, err := c.llm.Chat(ctx, req)
	if err != nil {
		c.logger.Warn("compression failed, continuing without compression", "error", err)
		return domain.WrapOp("compress", err)
	}

	summary := strings.TrimSpace(resp.Message.Content)
	if summary == "" {
		return nil
	}

	session.CompressMessages(summary, c.config.KeepRecent)
	c.logger.Info("conversation compressed",
		"original_count", len(msgs),
		"kept_recent", c.config.KeepRecent,
	)

	return nil
}

// compressSummaryName is used to identify compressed summary messages.
const compressSummaryName = "context_compression"

// CompressMessages replaces old messages with a summary message + the most recent N messages.
func (s *Session) CompressMessages(summaryContent string, keepRecent int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Msgs) <= keepRecent {
		return
	}

	recent := make([]domain.Message, keepRecent)
	copy(recent, s.Msgs[len(s.Msgs)-keepRecent:])

	summary := domain.Message{
		Role:      domain.RoleAssistant,
		Content:   summaryContent,
		Name:      compressSummaryName,
		Timestamp: time.Now(),
	}

	s.Msgs = append([]domain.Message{summary}, recent...)
	s.UpdatedAt = time.Now()
}

// MessageCount returns the number of messages without copying (thread-safe).
func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Msgs)
}
