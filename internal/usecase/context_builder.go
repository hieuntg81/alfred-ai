package usecase

import (
	"fmt"
	"strings"
	"time"

	"alfred-ai/internal/domain"
)

// ContextBuilder constructs the prompt message array for LLM calls.
type ContextBuilder struct {
	systemPrompt   string
	maxMessages    int
	model          string
	skills         []domain.Skill
	thinkingBudget int
}

// NewContextBuilder creates a new context builder.
func NewContextBuilder(systemPrompt, model string, maxMessages int) *ContextBuilder {
	return &ContextBuilder{
		systemPrompt: systemPrompt,
		model:        model,
		maxMessages:  maxMessages,
	}
}

// SetSkills sets the prompt-type skills to include in the system prompt.
func (cb *ContextBuilder) SetSkills(skills []domain.Skill) {
	cb.skills = skills
}

// SetThinkingBudget sets the extended thinking budget (in tokens) for LLM requests.
// A value of 0 disables extended thinking.
func (cb *ContextBuilder) SetThinkingBudget(budget int) {
	cb.thinkingBudget = budget
}

// Build assembles: system prompt + memory context + conversation history.
func (cb *ContextBuilder) Build(
	history []domain.Message,
	memoryContext []domain.MemoryEntry,
	tools []domain.ToolSchema,
) domain.ChatRequest {
	messages := make([]domain.Message, 0, 2+len(history))

	// System prompt (always first)
	systemContent := cb.systemPrompt
	if len(memoryContext) > 0 {
		systemContent += "\n\n## Relevant Memory Context\n" + cb.formatMemory(memoryContext)
	}
	if len(cb.skills) > 0 {
		systemContent += "\n\n## Available Skills\n" + cb.formatSkills()
	}
	messages = append(messages, domain.Message{
		Role:      domain.RoleSystem,
		Content:   systemContent,
		Timestamp: time.Now(),
	})

	// Repair broken tool chains, then truncate.
	hist := RepairTranscript(history)
	hist = cb.truncateHistory(hist)
	messages = append(messages, hist...)

	return domain.ChatRequest{
		Model:          cb.model,
		Messages:       messages,
		Tools:          tools,
		ThinkingBudget: cb.thinkingBudget,
	}
}

func (cb *ContextBuilder) formatMemory(entries []domain.MemoryEntry) string {
	var sb strings.Builder
	for i, entry := range entries {
		fmt.Fprintf(&sb, "- [%d] %s", i+1, entry.Content)
		if len(entry.Tags) > 0 {
			fmt.Fprintf(&sb, " (tags: %s)", strings.Join(entry.Tags, ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (cb *ContextBuilder) formatSkills() string {
	var sb strings.Builder
	for _, s := range cb.skills {
		fmt.Fprintf(&sb, "- **%s**: %s", s.Name, s.Description)
		if len(s.Tags) > 0 {
			fmt.Fprintf(&sb, " (tags: %s)", strings.Join(s.Tags, ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (cb *ContextBuilder) truncateHistory(history []domain.Message) []domain.Message {
	if cb.maxMessages <= 0 || len(history) <= cb.maxMessages {
		return history
	}

	// Partition messages into atomic groups so that
	// [Assistant(tool_calls), ToolResult...] are never split.
	groups := groupMessages(history)

	// Keep groups from the end until we exceed the message budget.
	var kept [][]domain.Message
	total := 0
	for i := len(groups) - 1; i >= 0; i-- {
		groupLen := len(groups[i])
		if total+groupLen > cb.maxMessages && total > 0 {
			break
		}
		kept = append(kept, groups[i])
		total += groupLen
	}

	// Reverse to restore chronological order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	// Flatten back to a message slice.
	result := make([]domain.Message, 0, total)
	for _, g := range kept {
		result = append(result, g...)
	}

	// Preserve compression summary at position 0 if present in original history.
	if len(history) > 0 && history[0].Name == compressSummaryName {
		if len(result) == 0 || result[0].Name != compressSummaryName {
			result = append([]domain.Message{history[0]}, result...)
		}
	}

	return result
}

// groupMessages partitions messages into atomic groups.
// An assistant message with tool calls and its immediately following
// tool result messages form a single group. All other messages are
// individual groups.
func groupMessages(msgs []domain.Message) [][]domain.Message {
	var groups [][]domain.Message
	i := 0
	for i < len(msgs) {
		msg := msgs[i]
		if msg.Role == domain.RoleAssistant && len(msg.ToolCalls) > 0 {
			// Start of an atomic group.
			group := []domain.Message{msg}
			j := i + 1
			for j < len(msgs) && msgs[j].Role == domain.RoleTool {
				group = append(group, msgs[j])
				j++
			}
			groups = append(groups, group)
			i = j
		} else {
			groups = append(groups, []domain.Message{msg})
			i++
		}
	}
	return groups
}
