// Package uxerror translates raw errors into user-friendly messages with
// recovery hints for the TUI.
package uxerror

import (
	"errors"
	"fmt"
	"strings"

	"alfred-ai/internal/adapter/tui/theme"
	"alfred-ai/internal/domain"
)

// FriendlyError is a user-facing error with suggestions for recovery.
type FriendlyError struct {
	Title   string   // short heading, e.g. "Connection Refused"
	Message string   // one-liner explanation
	Hints   []string // actionable recovery suggestions
	Raw     string   // original error text (for debug)
}

// Render formats the FriendlyError for display in the TUI message list.
func (fe FriendlyError) Render() string {
	var sb strings.Builder
	sb.WriteString(fe.Title)
	if fe.Message != "" {
		sb.WriteString("\n  ")
		sb.WriteString(fe.Message)
	}
	if len(fe.Hints) > 0 {
		sb.WriteString("\n  Suggestions:")
		for _, h := range fe.Hints {
			sb.WriteString(fmt.Sprintf("\n    %s %s", theme.SymbolBullet, h))
		}
	}
	return sb.String()
}

type errorPattern struct {
	match   func(err error) bool
	produce func(err error) FriendlyError
}

var patterns = []errorPattern{
	// Domain sentinel errors (checked first so errors.Is works through wrapping).
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrMaxIterations) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Agent Loop Limit Reached",
				Message: "The agent exceeded its maximum number of iterations.",
				Hints:   []string{"Break the task into smaller steps", "Increase max_iterations in config"},
				Raw:     err.Error(),
			}
		},
	},
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrPathOutsideSandbox) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Sandbox Violation",
				Message: "A tool tried to access a path outside the allowed sandbox.",
				Hints:   []string{"Check the sandbox.root setting in config", "Use paths within the sandbox directory"},
				Raw:     err.Error(),
			}
		},
	},
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrCommandNotAllowed) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Command Blocked",
				Message: "A shell command was blocked by the security policy.",
				Hints:   []string{"Add the command to sandbox.command_allowlist", "Review security settings"},
				Raw:     err.Error(),
			}
		},
	},
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrToolApprovalDenied) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Tool Execution Denied",
				Message: "The tool call was not approved.",
				Hints:   []string{"Approve the tool when prompted", "Adjust tool_approval settings"},
				Raw:     err.Error(),
			}
		},
	},
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrToolApprovalTimeout) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Tool Approval Timed Out",
				Message: "You didn't respond to the tool approval prompt in time.",
				Hints:   []string{"Try again and respond to the approval prompt", "Increase approval timeout in config"},
				Raw:     err.Error(),
			}
		},
	},
	{
		match: func(err error) bool { return errors.Is(err, domain.ErrSSRFBlocked) },
		produce: func(err error) FriendlyError {
			return FriendlyError{
				Title:   "Request Blocked (SSRF)",
				Message: "A network request to a private/reserved IP was blocked.",
				Hints:   []string{"Use public URLs only", "Check if the target service is reachable"},
				Raw:     err.Error(),
			}
		},
	},

	// Network / connectivity patterns (string matching for external errors).
	{
		match:   containsAny("connection refused", "dial tcp", "no such host"),
		produce: constantError("Connection Failed", "Could not reach the remote service.", []string{"Check your internet connection", "Verify the service URL in config", "Check if a firewall is blocking the connection"}),
	},
	{
		match:   containsAny("deadline exceeded", "timeout", "context deadline"),
		produce: constantError("Request Timed Out", "The request took too long to complete.", []string{"Try a shorter prompt or simpler task", "Check your network connection", "Increase timeout in config"}),
	},

	// Auth patterns.
	{
		match:   containsAny("401", "unauthorized", "invalid api key", "authentication failed", "invalid x-api-key"),
		produce: constantError("Authentication Failed", "The API key or credentials were rejected.", []string{"Run 'alfred-ai setup' to reconfigure", "Check your API key environment variable", "Verify the key hasn't expired"}),
	},

	// Rate limiting.
	{
		match:   containsAny("429", "rate limit", "too many requests"),
		produce: constantError("Rate Limited", "Too many requests sent to the API provider.", []string{"Wait a moment before retrying", "Consider upgrading your API plan", "Reduce request frequency"}),
	},

	// Quota / billing.
	{
		match:   containsAny("402", "quota", "billing", "insufficient"),
		produce: constantError("Quota Exceeded", "Your API quota or billing limit has been reached.", []string{"Check your API provider billing dashboard", "Upgrade your plan or add credits"}),
	},
}

// Humanize converts a raw error into a FriendlyError with recovery hints.
func Humanize(err error) FriendlyError {
	if err == nil {
		return FriendlyError{Title: "Unknown Error", Raw: "nil"}
	}

	for _, p := range patterns {
		if p.match(err) {
			return p.produce(err)
		}
	}

	// Fallback for unrecognized errors.
	return FriendlyError{
		Title:   "Unexpected Error",
		Message: err.Error(),
		Hints:   []string{"Try again", "Run with ALFREDAI_DEBUG=1 for more details"},
		Raw:     err.Error(),
	}
}

// containsAny returns a match func that checks if the error string contains
// any of the given substrings (case-insensitive).
func containsAny(substrs ...string) func(error) bool {
	return func(err error) bool {
		lower := strings.ToLower(err.Error())
		for _, s := range substrs {
			if strings.Contains(lower, s) {
				return true
			}
		}
		return false
	}
}

// constantError returns a produce func that always returns the same FriendlyError.
func constantError(title, message string, hints []string) func(error) FriendlyError {
	return func(err error) FriendlyError {
		return FriendlyError{
			Title:   title,
			Message: message,
			Hints:   hints,
			Raw:     err.Error(),
		}
	}
}
