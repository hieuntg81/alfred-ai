package domain

import (
	"context"
	"encoding/json"
	"time"
)

// EventType identifies the kind of event being published.
type EventType string

const (
	EventMessageReceived   EventType = "message.received"
	EventMessageSent       EventType = "message.sent"
	EventToolCallStarted   EventType = "tool.call.started"
	EventToolCallCompleted EventType = "tool.call.completed"
	EventToolApprovalReq   EventType = "tool.approval.request"
	EventToolApprovalResp  EventType = "tool.approval.response"
	EventLLMCallStarted    EventType = "llm.call.started"
	EventLLMCallCompleted  EventType = "llm.call.completed"
	EventStreamDelta       EventType = "stream.delta"
	EventStreamStarted    EventType = "stream.started"
	EventStreamCompleted  EventType = "stream.completed"
	EventStreamError      EventType = "stream.error"
	EventSessionCreated    EventType = "session.created"
	EventSessionDeleted    EventType = "session.deleted"
	EventMemoryStored      EventType = "memory.stored"
	EventMemoryDeleted     EventType = "memory.deleted"
	EventPluginLoaded      EventType = "plugin.loaded"
	EventPluginUnloaded    EventType = "plugin.unloaded"
	EventAgentError        EventType = "agent.error"
	EventAgentDelegated    EventType = "agent.delegated"
	EventAgentRouted       EventType = "agent.routed"

	// Node system events (Phase 5).
	EventNodeRegistered   EventType = "node.registered"
	EventNodeUnregistered EventType = "node.unregistered"
	EventNodeInvoked      EventType = "node.invoked"
	EventNodeHeartbeat    EventType = "node.heartbeat"
	EventNodeUnreachable  EventType = "node.unreachable"
	EventNodeDiscovered   EventType = "node.discovered"

	// Chat lifecycle events (Phase 6).
	EventChatAborted EventType = "chat.aborted"

	// Canvas tool events.
	EventCanvasCreated   EventType = "canvas.created"
	EventCanvasUpdated   EventType = "canvas.updated"
	EventCanvasDeleted   EventType = "canvas.deleted"
	EventCanvasPresented EventType = "canvas.presented"
	EventCanvasHidden    EventType = "canvas.hidden"
	EventCanvasEvalJS    EventType = "canvas.eval_js"

	// Cron job events.
	EventCronJobCreated EventType = "cron.job.created"
	EventCronJobUpdated EventType = "cron.job.updated"
	EventCronJobDeleted EventType = "cron.job.deleted"
	EventCronJobFired   EventType = "cron.job.fired"

	// Process management events.
	EventProcessStarted   EventType = "process.started"
	EventProcessCompleted EventType = "process.completed"
	EventProcessKilled    EventType = "process.killed"

	// Workflow engine events.
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventWorkflowPaused    EventType = "workflow.paused"
	EventWorkflowResumed   EventType = "workflow.resumed"
)

// Event is the envelope published on the event bus.
type Event struct {
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// EventHandler is a callback invoked when an event is received.
type EventHandler func(ctx context.Context, event Event)

// EventBus provides a publish/subscribe mechanism for domain events.
type EventBus interface {
	// Publish sends an event to all matching subscribers.
	Publish(ctx context.Context, event Event)
	// Subscribe registers a handler for a specific event type.
	// Returns an unsubscribe function.
	Subscribe(eventType EventType, handler EventHandler) func()
	// SubscribeAll registers a handler that receives every event.
	// Returns an unsubscribe function.
	SubscribeAll(handler EventHandler) func()
	// Close drains in-flight handlers and prevents new publishes.
	Close()
}
