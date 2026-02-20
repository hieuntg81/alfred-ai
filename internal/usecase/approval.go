package usecase

import (
	"context"
	"fmt"

	"alfred-ai/internal/domain"
)

// ConfigApprover is a ToolApprover driven by allow/deny lists.
//
// Security Model (Secure by Default):
//   - Tools in alwaysApprove: Auto-approved without user interaction
//   - Tools in alwaysDeny: Always rejected
//   - Unknown/unlisted tools: DENIED by default (fail-safe)
//
// This implements defense-in-depth: tools must be explicitly allowed to execute.
// Interactive approval (prompting user) is not yet implemented, so unlisted tools
// are automatically denied for security reasons.
//
// Example usage:
//
//	approver := NewConfigApprover(
//	    []string{"read_file", "web_fetch"},  // Safe tools - auto-approve
//	    []string{"execute_shell"},           // Dangerous tools - always deny
//	)
//	// Any tool not in these lists will be denied by default
type ConfigApprover struct {
	alwaysApprove map[string]bool
	alwaysDeny    map[string]bool
}

// NewConfigApprover creates a ConfigApprover from allow/deny lists.
func NewConfigApprover(approve, deny []string) *ConfigApprover {
	a := &ConfigApprover{
		alwaysApprove: make(map[string]bool, len(approve)),
		alwaysDeny:    make(map[string]bool, len(deny)),
	}
	for _, name := range approve {
		a.alwaysApprove[name] = true
	}
	for _, name := range deny {
		a.alwaysDeny[name] = true
	}
	return a
}

// NeedsApproval returns false if the tool is in the always-approve list,
// true otherwise (including always-deny and unknown tools).
func (c *ConfigApprover) NeedsApproval(call domain.ToolCall) bool {
	if c.alwaysApprove[call.Name] {
		return false
	}
	return true
}

// RequestApproval implements the approval decision logic.
//
// Decision Flow:
//  1. If tool is in alwaysDeny → REJECT immediately
//  2. If tool is in alwaysApprove → APPROVE immediately
//  3. Otherwise → DENY by default (secure fail-safe)
//
// Security Rationale:
// Unknown tools are denied by default to prevent unauthorized execution.
// This is a fail-safe design: better to deny a legitimate tool than to
// accidentally allow a dangerous operation.
//
// Future Enhancement:
// Interactive approval (prompting user via CLI/UI) is not yet implemented.
// When implemented, step 3 will become: "prompt user for approval".
// Until then, tools must be explicitly added to alwaysApprove list.
func (c *ConfigApprover) RequestApproval(_ context.Context, call domain.ToolCall) (bool, error) {
	if c.alwaysDeny[call.Name] {
		return false, domain.ErrToolApprovalDenied
	}

	if c.alwaysApprove[call.Name] {
		return true, nil
	}

	// Default: DENY (interactive approval not implemented yet)
	return false, domain.NewDomainError(
		"ConfigApprover.RequestApproval",
		domain.ErrToolApprovalDenied,
		fmt.Sprintf("tool %q requires approval but interactive mode not implemented", call.Name),
	)
}
