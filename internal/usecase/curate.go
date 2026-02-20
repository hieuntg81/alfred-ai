package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"alfred-ai/internal/domain"
)

const curateSystemPrompt = `You are a knowledge extraction assistant. Analyze the conversation and extract important facts, preferences, decisions, or technical knowledge worth remembering long-term.

For each piece of knowledge, output exactly in this format:
POINT: <concise fact or preference>
TAGS: <comma-separated tags>

Rules:
- Only extract genuinely useful, long-term knowledge
- Skip greetings, small talk, and transient information
- Each POINT should be self-contained and understandable without context
- Tags should be lowercase, relevant keywords
- If the conversation contains nothing worth remembering, respond with exactly: NONE`

// CuratorOption configures a Curator.
type CuratorOption func(*Curator)

// WithEmbedder sets an optional embedding provider for future batch embedding.
func WithEmbedder(embedder domain.EmbeddingProvider) CuratorOption {
	return func(c *Curator) { c.embedder = embedder }
}

// Curator extracts knowledge from conversations and stores it in memory.
type Curator struct {
	memory   domain.MemoryProvider
	llm      domain.LLMProvider
	logger   *slog.Logger
	embedder domain.EmbeddingProvider // optional, reserved for future use
}

// NewCurator creates a new Curator.
func NewCurator(memory domain.MemoryProvider, llm domain.LLMProvider, logger *slog.Logger, opts ...CuratorOption) *Curator {
	c := &Curator{
		memory: memory,
		llm:    llm,
		logger: logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CurateConversation analyzes messages and stores extracted knowledge.
func (c *Curator) CurateConversation(ctx context.Context, messages []domain.Message) (*domain.CurateResult, error) {
	if len(messages) == 0 {
		return &domain.CurateResult{}, nil
	}

	// Build conversation text for the LLM
	var sb strings.Builder
	for _, msg := range messages {
		if msg.Role == domain.RoleSystem || msg.Role == domain.RoleTool {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n", msg.Role, msg.Content)
	}

	conversationText := sb.String()
	if strings.TrimSpace(conversationText) == "" {
		return &domain.CurateResult{}, nil
	}

	// Ask LLM to extract knowledge
	req := domain.ChatRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: curateSystemPrompt},
			{Role: domain.RoleUser, Content: conversationText},
		},
		Temperature: 0.3,
	}

	resp, err := c.llm.Chat(ctx, req)
	if err != nil {
		return nil, domain.NewDomainError("Curator.CurateConversation", domain.ErrCurateFailed, err.Error())
	}

	// Parse LLM response
	points := parseCurateResponse(resp.Message.Content)
	if len(points) == 0 {
		return &domain.CurateResult{Skipped: 1, Summary: "no knowledge extracted"}, nil
	}

	// Store each extracted point
	result := &domain.CurateResult{}
	var allKeywords []string

	for _, p := range points {
		entry := domain.MemoryEntry{
			Content:  p.content,
			Tags:     p.tags,
			Metadata: map[string]string{"source": "auto-curate"},
		}

		if err := c.memory.Store(ctx, entry); err != nil {
			c.logger.Warn("failed to store curated entry", "error", err)
			result.Skipped++
			continue
		}

		result.Stored++
		allKeywords = append(allKeywords, p.tags...)
	}

	result.Keywords = uniqueStrings(allKeywords)
	result.Summary = fmt.Sprintf("extracted %d knowledge points", result.Stored)

	c.logger.Info("curation complete",
		"stored", result.Stored,
		"skipped", result.Skipped,
		"keywords", result.Keywords,
	)

	return result, nil
}

// curatePoint holds a single extracted knowledge point.
type curatePoint struct {
	content string
	tags    []string
}

// parseCurateResponse parses the LLM's POINT:/TAGS: format output.
func parseCurateResponse(response string) []curatePoint {
	response = strings.TrimSpace(response)
	if response == "NONE" || response == "" {
		return nil
	}

	var points []curatePoint
	lines := strings.Split(response, "\n")

	var currentContent string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "POINT:") {
			currentContent = strings.TrimSpace(strings.TrimPrefix(line, "POINT:"))
		} else if strings.HasPrefix(line, "TAGS:") && currentContent != "" {
			tagsRaw := strings.TrimSpace(strings.TrimPrefix(line, "TAGS:"))
			tags := parseTags(tagsRaw)
			points = append(points, curatePoint{
				content: currentContent,
				tags:    tags,
			})
			currentContent = ""
		}
	}

	return points
}

// parseTags splits comma-separated tags, trims whitespace, lowercases.
func parseTags(s string) []string {
	parts := strings.Split(s, ",")
	var tags []string
	for _, p := range parts {
		tag := strings.TrimSpace(strings.ToLower(p))
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// uniqueStrings deduplicates a string slice.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	var result []string
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}
