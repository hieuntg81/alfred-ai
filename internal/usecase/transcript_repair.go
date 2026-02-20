package usecase

import (
	"time"

	"alfred-ai/internal/domain"
)

// RepairTranscript scans the message history and fixes broken tool chains:
//  1. If an Assistant message has ToolCalls but the next message is NOT a
//     matching ToolResult, inject an error ToolResult.
//  2. If a ToolResult appears without a preceding Assistant tool_call,
//     remove the orphan.
//
// Returns a new slice (does not modify the input).
func RepairTranscript(messages []domain.Message) []domain.Message {
	if len(messages) == 0 {
		return messages
	}

	result := make([]domain.Message, 0, len(messages))
	pendingCalls := make(map[string]domain.ToolCall) // callID → ToolCall

	for _, msg := range messages {
		switch msg.Role {
		case domain.RoleAssistant:
			// Before adding this assistant message, inject missing results
			// for any still-pending calls from a PREVIOUS assistant message.
			result = injectMissingResults(result, pendingCalls)
			clear(pendingCalls)

			// Track new tool calls from this assistant message.
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					pendingCalls[tc.ID] = tc
				}
			}
			result = append(result, msg)

		case domain.RoleTool:
			// Check if this tool result matches a pending call.
			callID := extractRepairToolCallID(msg)
			if callID == "" {
				// No call ID — orphaned, drop it.
				continue
			}
			if _, ok := pendingCalls[callID]; ok {
				delete(pendingCalls, callID)
				result = append(result, msg)
			} else {
				// No matching call — orphaned, drop it.
				continue
			}

		default:
			// User or system messages: inject missing results for pending calls
			// then reset (new conversational turn).
			result = injectMissingResults(result, pendingCalls)
			clear(pendingCalls)
			result = append(result, msg)
		}
	}

	// Handle any remaining pending calls at the end.
	result = injectMissingResults(result, pendingCalls)

	return result
}

// injectMissingResults appends error ToolResult messages for each pending
// tool call that did not receive a result.
func injectMissingResults(msgs []domain.Message, pending map[string]domain.ToolCall) []domain.Message {
	for id, tc := range pending {
		msgs = append(msgs, domain.Message{
			Role:    domain.RoleTool,
			Name:    tc.Name,
			Content: "[error] tool call did not produce a result",
			ToolCalls: []domain.ToolCall{{
				ID:   id,
				Name: tc.Name,
			}},
			Timestamp: time.Now(),
		})
	}
	return msgs
}

// extractRepairToolCallID extracts the tool call ID from a tool result message.
func extractRepairToolCallID(msg domain.Message) string {
	if len(msg.ToolCalls) > 0 {
		return msg.ToolCalls[0].ID
	}
	return ""
}
