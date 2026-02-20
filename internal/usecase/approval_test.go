package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"alfred-ai/internal/domain"
)

func TestConfigApproverAlwaysApprove(t *testing.T) {
	approver := NewConfigApprover([]string{"filesystem", "web"}, nil)

	call := domain.ToolCall{Name: "filesystem", Arguments: json.RawMessage(`{}`)}
	if approver.NeedsApproval(call) {
		t.Error("expected filesystem to NOT need approval")
	}

	approved, err := approver.RequestApproval(context.Background(), call)
	if err != nil || !approved {
		t.Errorf("expected approval, got approved=%v err=%v", approved, err)
	}
}

func TestConfigApproverAlwaysDeny(t *testing.T) {
	approver := NewConfigApprover(nil, []string{"shell"})

	call := domain.ToolCall{Name: "shell", Arguments: json.RawMessage(`{}`)}
	if !approver.NeedsApproval(call) {
		t.Error("expected shell to need approval")
	}

	approved, err := approver.RequestApproval(context.Background(), call)
	if approved {
		t.Error("expected denial")
	}
	if !errors.Is(err, domain.ErrToolApprovalDenied) {
		t.Errorf("expected ErrToolApprovalDenied, got %v", err)
	}
}

func TestConfigApproverDefault(t *testing.T) {
	approver := NewConfigApprover(nil, nil)

	call := domain.ToolCall{Name: "unknown_tool", Arguments: json.RawMessage(`{}`)}
	if !approver.NeedsApproval(call) {
		t.Error("expected unknown tool to need approval")
	}

	// Default for unknown tools: DENY (interactive approval not implemented)
	approved, err := approver.RequestApproval(context.Background(), call)
	if approved {
		t.Error("expected denial for unknown tool")
	}
	if !errors.Is(err, domain.ErrToolApprovalDenied) {
		t.Errorf("expected ErrToolApprovalDenied, got %v", err)
	}
}

func TestConfigApproverMixed(t *testing.T) {
	approver := NewConfigApprover([]string{"read"}, []string{"delete"})

	// read: always approve
	if approver.NeedsApproval(domain.ToolCall{Name: "read"}) {
		t.Error("read should not need approval")
	}

	// delete: always deny
	if !approver.NeedsApproval(domain.ToolCall{Name: "delete"}) {
		t.Error("delete should need approval")
	}
	approved, err := approver.RequestApproval(context.Background(), domain.ToolCall{Name: "delete"})
	if approved {
		t.Error("delete should be denied")
	}
	if !errors.Is(err, domain.ErrToolApprovalDenied) {
		t.Errorf("expected ErrToolApprovalDenied for delete, got %v", err)
	}

	// other: needs approval, denied by default (interactive approval not implemented)
	if !approver.NeedsApproval(domain.ToolCall{Name: "other"}) {
		t.Error("other should need approval")
	}
	approved, err = approver.RequestApproval(context.Background(), domain.ToolCall{Name: "other"})
	if approved {
		t.Error("other should be denied (no interactive approval)")
	}
	if !errors.Is(err, domain.ErrToolApprovalDenied) {
		t.Errorf("expected ErrToolApprovalDenied for other, got %v", err)
	}
}
