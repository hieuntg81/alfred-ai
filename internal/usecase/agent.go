package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// Recovery loop constants.
const (
	maxLLMRetries  = 3
	baseRetryDelay = 500 * time.Millisecond
	maxRetryDelay  = 10 * time.Second
)

// AgentDeps holds injected dependencies for the agent.
type AgentDeps struct {
	LLM             domain.LLMProvider
	Memory          domain.MemoryProvider
	Tools           domain.ToolExecutor
	ContextBuilder  *ContextBuilder
	Logger          *slog.Logger
	MaxIterations   int
	AuditLogger     domain.AuditLogger   // optional, nil = no audit
	Compressor      *Compressor          // optional, nil = no compression
	Bus             domain.EventBus      // optional, nil = no events
	Approver        domain.ToolApprover  // optional, nil = no approval gating
	Identity        domain.AgentIdentity // optional, for multi-agent mode
	ErrorClassifier *ErrorClassifier     // optional, nil = no error recovery
	SessionLocker   *SessionLocker       // optional, nil = no session locking
	ContextGuard    *ContextGuard        // optional, nil = no context window guard
}

// Agent orchestrates the receive-think-act loop.
type Agent struct {
	deps AgentDeps
}

// NewAgent creates an agent with the given dependencies.
func NewAgent(deps AgentDeps) *Agent {
	// Identity.MaxIter overrides MaxIterations when set.
	if deps.Identity.MaxIter > 0 {
		deps.MaxIterations = deps.Identity.MaxIter
	}
	if deps.MaxIterations <= 0 {
		deps.MaxIterations = 10
	}
	return &Agent{deps: deps}
}

// HandleMessage processes a single user message through the agent loop.
func (a *Agent) HandleMessage(ctx context.Context, session *Session, userMsg string) (string, error) {
	return a.handleInner(ctx, session, userMsg, nil)
}

// executeTool runs a single tool call and returns the result as a Message.
func (a *Agent) executeTool(ctx context.Context, sessionID string, call domain.ToolCall) domain.Message {
	ctx, span := tracer.StartSpan(ctx, "agent.execute_tool",
		trace.WithAttributes(tracer.StringAttr("tool.name", call.Name)),
	)
	defer span.End()

	tool, err := a.deps.Tools.Get(call.Name)
	if err != nil {
		tracer.RecordError(span, err)
		return domain.Message{
			Role:    domain.RoleTool,
			Name:    call.Name,
			Content: err.Error(),
			ToolCalls: []domain.ToolCall{{
				ID:   call.ID,
				Name: call.Name,
			}},
			Timestamp: time.Now(),
		}
	}

	// Tool approval gating
	if a.deps.Approver != nil && a.deps.Approver.NeedsApproval(call) {
		a.publishEvent(ctx, domain.EventToolApprovalReq, sessionID, nil)
		approved, approvalErr := a.deps.Approver.RequestApproval(ctx, call)
		a.publishEvent(ctx, domain.EventToolApprovalResp, sessionID, nil)
		if approvalErr != nil || !approved {
			msg := "tool call denied by approval policy"
			if approvalErr != nil {
				msg = approvalErr.Error()
			}
			return domain.Message{
				Role:    domain.RoleTool,
				Name:    call.Name,
				Content: msg,
				ToolCalls: []domain.ToolCall{{
					ID:   call.ID,
					Name: call.Name,
				}},
				Timestamp: time.Now(),
			}
		}
	}

	a.publishEvent(ctx, domain.EventToolCallStarted, sessionID, map[string]string{"tool": call.Name})
	result, err := tool.Execute(ctx, call.Arguments)
	a.publishEvent(ctx, domain.EventToolCallCompleted, sessionID, map[string]string{
		"tool":    call.Name,
		"success": fmt.Sprintf("%v", err == nil),
	})

	// Audit: log tool execution (no arguments/result content)
	if a.deps.AuditLogger != nil {
		success := "true"
		if err != nil {
			success = "false"
		}
		a.deps.AuditLogger.Log(ctx, domain.AuditEvent{
			Type: domain.AuditToolExec,
			Detail: map[string]string{
				"tool":    call.Name,
				"success": success,
			},
		})
	}

	if err != nil {
		tracer.RecordError(span, err)
		return domain.Message{
			Role:    domain.RoleTool,
			Name:    call.Name,
			Content: err.Error(),
			ToolCalls: []domain.ToolCall{{
				ID:   call.ID,
				Name: call.Name,
			}},
			Timestamp: time.Now(),
		}
	}

	tracer.SetOK(span)
	return domain.Message{
		Role:    domain.RoleTool,
		Name:    call.Name,
		Content: result.Content,
		ToolCalls: []domain.ToolCall{{
			ID:   call.ID,
			Name: call.Name,
		}},
		Timestamp: time.Now(),
	}
}

// retryBackoff computes exponential backoff with jitter.
func retryBackoff(attempt int) time.Duration {
	delay := baseRetryDelay * time.Duration(1<<uint(attempt))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	// Add 0-25% jitter.
	jitter := time.Duration(rand.Int63n(int64(delay/4) + 1))
	return delay + jitter
}

// publishEvent publishes a domain event on the bus if it is configured.
func (a *Agent) publishEvent(ctx context.Context, eventType domain.EventType, sessionID string, payload any) {
	publishEvent(a.deps.Bus, ctx, eventType, sessionID, payload)
}

// HandleMessageStream processes a user message with token-by-token streaming.
// It publishes EventStreamDelta events for each LLM chunk. If the LLM provider
// does not implement StreamingLLMProvider, it falls back to HandleMessage
// and emits a single EventStreamCompleted with the full response.
func (a *Agent) HandleMessageStream(ctx context.Context, session *Session, userMsg string) (string, error) {
	sp, canStream := a.deps.LLM.(domain.StreamingLLMProvider)
	if !canStream {
		// Fallback: run synchronous path, emit completed event with full response.
		result, err := a.HandleMessage(ctx, session, userMsg)
		if err == nil {
			a.publishEvent(ctx, domain.EventStreamCompleted, session.ID, domain.StreamCompletedPayload{
				Content: result,
			})
		}
		return result, err
	}

	return a.handleInner(ctx, session, userMsg, sp)
}

// handleInner is the shared agent loop for both sync and streaming modes.
// When sp is non-nil, it uses streaming via ChatStream; when sp is nil, it
// uses synchronous Chat.
func (a *Agent) handleInner(ctx context.Context, session *Session, userMsg string, sp domain.StreamingLLMProvider) (string, error) {
	streaming := sp != nil

	spanName := "agent.handle_message"
	opName := "Agent.HandleMessage"
	if streaming {
		spanName = "agent.handle_message_stream"
		opName = "Agent.HandleMessageStream"
	}

	ctx, span := tracer.StartSpan(ctx, spanName)
	defer span.End()

	// Acquire session lock if available.
	if a.deps.SessionLocker != nil {
		unlock, lockErr := a.deps.SessionLocker.Lock(ctx, session.ID)
		if lockErr != nil {
			return "", domain.NewDomainError(opName, lockErr, "session lock")
		}
		defer unlock()
	}

	ctx = domain.ContextWithSessionID(ctx, session.ID)

	// Add user message to session.
	session.AddMessage(domain.Message{
		Role:      domain.RoleUser,
		Content:   userMsg,
		Timestamp: time.Now(),
	})

	// Context guard check point 1: after adding user message.
	if a.deps.ContextGuard != nil {
		if err := a.deps.ContextGuard.Check(ctx, session); err != nil {
			return "", err
		}
	}

	// Query memory for relevant context.
	var memories []domain.MemoryEntry
	if a.deps.Memory != nil && a.deps.Memory.IsAvailable() {
		memCtx, memSpan := tracer.StartSpan(ctx, "agent.query_memory")
		var err error
		memories, err = a.deps.Memory.Query(memCtx, userMsg, 5)
		memSpan.End()
		if err != nil {
			a.deps.Logger.Warn("memory query failed", "error", err)
		}
	}

	if streaming {
		a.publishEvent(ctx, domain.EventStreamStarted, session.ID, nil)
	}

	var totalUsage domain.Usage

	// Agent loop.
	for i := 0; i < a.deps.MaxIterations; i++ {
		if streaming && ctx.Err() != nil {
			return "", ctx.Err()
		}

		iterEvent := "agent.iteration"
		if streaming {
			iterEvent = "agent.stream_iteration"
		}
		span.AddEvent(iterEvent, trace.WithAttributes(tracer.IntAttr("iteration", i)))

		chatReq := a.deps.ContextBuilder.Build(
			session.Messages(), memories, a.deps.Tools.Schemas(),
		)

		a.publishEvent(ctx, domain.EventLLMCallStarted, session.ID, nil)

		// Call LLM with retry logic.
		msg, usage, llmErr := a.callLLMWithRetry(ctx, session, chatReq, memories, sp, i)

		if llmErr != nil {
			if streaming {
				a.publishEvent(ctx, domain.EventStreamError, session.ID, domain.StreamErrorPayload{
					Error: llmErr.Error(),
				})
			}
			a.publishEvent(ctx, domain.EventAgentError, session.ID, map[string]string{"error": llmErr.Error()})
			tracer.RecordError(span, llmErr)
			return "", llmErr
		}
		a.publishEvent(ctx, domain.EventLLMCallCompleted, session.ID, nil)

		totalUsage.PromptTokens += usage.PromptTokens
		totalUsage.CompletionTokens += usage.CompletionTokens
		totalUsage.TotalTokens += usage.TotalTokens

		// Audit: log LLM call (no content).
		if a.deps.AuditLogger != nil {
			a.deps.AuditLogger.Log(ctx, domain.AuditEvent{
				Type: domain.AuditLLMCall,
				Detail: map[string]string{
					"model":             a.deps.LLM.Name(),
					"prompt_tokens":     fmt.Sprintf("%d", usage.PromptTokens),
					"completion_tokens": fmt.Sprintf("%d", usage.CompletionTokens),
					"total_tokens":      fmt.Sprintf("%d", usage.TotalTokens),
				},
			})
		}

		session.AddMessage(msg)

		logMsg := "llm response"
		if streaming {
			logMsg = "llm stream response"
		}
		a.deps.Logger.Debug(logMsg,
			"iteration", i,
			"tool_calls", len(msg.ToolCalls),
			"tokens", usage.TotalTokens,
		)

		// No tool calls = final response.
		if len(msg.ToolCalls) == 0 {
			if a.deps.Compressor != nil && a.deps.Compressor.ShouldCompress(session) {
				if err := a.deps.Compressor.Compress(ctx, session); err != nil {
					a.deps.Logger.Warn("compression failed", "error", err)
				}
			}
			if streaming {
				a.publishEvent(ctx, domain.EventStreamCompleted, session.ID, domain.StreamCompletedPayload{
					Content: msg.Content,
					Usage:   &totalUsage,
				})
			}
			tracer.SetOK(span)
			return msg.Content, nil
		}

		// Execute tool calls in parallel.
		// Results are collected in an indexed array to preserve original call order.
		toolMsgs := make([]domain.Message, len(msg.ToolCalls))
		var wg sync.WaitGroup
		for i, call := range msg.ToolCalls {
			wg.Add(1)
			go func(idx int, c domain.ToolCall) {
				defer wg.Done()
				toolMsgs[idx] = a.executeTool(ctx, session.ID, c)
			}(i, call)
		}
		wg.Wait()
		for _, toolMsg := range toolMsgs {
			session.AddMessage(toolMsg)
		}

		// Context guard check point 2: after adding tool results.
		if a.deps.ContextGuard != nil {
			if err := a.deps.ContextGuard.Check(ctx, session); err != nil {
				return "", err
			}
		}
	}

	if streaming {
		a.publishEvent(ctx, domain.EventStreamError, session.ID, domain.StreamErrorPayload{
			Error: domain.ErrMaxIterations.Error(),
		})
	}
	tracer.RecordError(span, domain.ErrMaxIterations)
	return "", domain.ErrMaxIterations
}

// callLLMWithRetry performs the LLM call with retry logic for both sync and
// streaming modes. When sp is non-nil, it uses ChatStream and accumulates
// deltas; when sp is nil, it uses Chat directly.
func (a *Agent) callLLMWithRetry(
	ctx context.Context,
	session *Session,
	chatReq domain.ChatRequest,
	memories []domain.MemoryEntry,
	sp domain.StreamingLLMProvider,
	iteration int,
) (domain.Message, domain.Usage, error) {
	streaming := sp != nil

	maxAttempts := 1
	if a.deps.ErrorClassifier != nil {
		maxAttempts = maxLLMRetries
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var msg domain.Message
		var usage domain.Usage
		var callErr error

		if streaming {
			llmCtx, llmSpan := tracer.StartSpan(ctx, "agent.llm_stream")
			deltaCh, err := sp.ChatStream(llmCtx, chatReq)
			llmSpan.End()

			if err != nil {
				callErr = err
			} else {
				acc := newStreamAccumulator()
				for delta := range deltaCh {
					acc.addDelta(delta)
					a.publishEvent(ctx, domain.EventStreamDelta, session.ID, domain.StreamDeltaPayload{
						Content:   delta.Content,
						ToolCalls: delta.ToolCalls,
						Done:      delta.Done,
						Iteration: iteration,
					})
				}
				msg, usage = acc.build()
			}
		} else {
			llmCtx, llmSpan := tracer.StartSpan(ctx, "agent.llm_call")
			resp, err := a.deps.LLM.Chat(llmCtx, chatReq)
			llmSpan.End()

			if err != nil {
				callErr = err
			} else {
				msg = resp.Message
				usage = resp.Usage
			}
		}

		if callErr == nil {
			return msg, usage, nil
		}
		lastErr = callErr

		// No classifier → fail immediately.
		if a.deps.ErrorClassifier == nil {
			return domain.Message{}, domain.Usage{}, lastErr
		}

		classified := a.deps.ErrorClassifier.Classify(callErr)
		if classified.Category != ErrorCategoryRetryable {
			return domain.Message{}, domain.Usage{}, lastErr
		}

		// Context overflow: try force compression, rebuild prompt.
		if errors.Is(classified.Sentinel, domain.ErrContextOverflow) && a.deps.Compressor != nil {
			if compErr := a.deps.Compressor.ForceCompress(ctx, session); compErr != nil {
				a.deps.Logger.Warn("force compression failed", "error", compErr)
			}
			chatReq = a.deps.ContextBuilder.Build(
				session.Messages(), memories, a.deps.Tools.Schemas(),
			)
			continue
		}

		// Rate limit or server error: exponential backoff with jitter.
		if attempt < maxAttempts-1 {
			delay := retryBackoff(attempt)
			mode := "LLM call"
			if streaming {
				mode = "LLM stream"
			}
			a.deps.Logger.Info("retrying "+mode+" after error",
				"attempt", attempt+1, "delay", delay, "error", callErr)
			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return domain.Message{}, domain.Usage{}, ctx.Err()
			}
		}
	}

	return domain.Message{}, domain.Usage{}, lastErr
}

// maxToolCallsPerDelta limits the number of tool call slots the accumulator
// will allocate. Indices beyond this bound are silently dropped to prevent
// memory exhaustion from malformed streaming deltas.
const maxToolCallsPerDelta = 50

// streamAccumulator collects incremental deltas into a complete message.
type streamAccumulator struct {
	content   strings.Builder
	toolCalls []domain.ToolCall // accumulated by index
	usage     domain.Usage
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{}
}

// addDelta merges a single streaming delta into the accumulator.
// Tool calls are tracked by index (position in delta.ToolCalls array).
// The first delta for a tool call provides ID and Name; subsequent deltas
// append to Arguments.
func (acc *streamAccumulator) addDelta(delta domain.StreamDelta) {
	acc.content.WriteString(delta.Content)

	for idx, tc := range delta.ToolCalls {
		if idx >= maxToolCallsPerDelta {
			break // defensive bound — skip excessively large indices
		}

		// Grow slice to accommodate this index.
		for len(acc.toolCalls) <= idx {
			acc.toolCalls = append(acc.toolCalls, domain.ToolCall{})
		}

		existing := &acc.toolCalls[idx]
		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Name != "" {
			existing.Name = tc.Name
		}
		if len(tc.Arguments) > 0 {
			existing.Arguments = append(existing.Arguments, tc.Arguments...)
		}
	}

	if delta.Usage != nil {
		acc.usage.PromptTokens = delta.Usage.PromptTokens
		acc.usage.CompletionTokens = delta.Usage.CompletionTokens
		acc.usage.TotalTokens = delta.Usage.TotalTokens
	}
}

// build returns the accumulated message and usage.
func (acc *streamAccumulator) build() (domain.Message, domain.Usage) {
	msg := domain.Message{
		Role:      domain.RoleAssistant,
		Content:   acc.content.String(),
		ToolCalls: acc.toolCalls,
		Timestamp: time.Now(),
	}
	return msg, acc.usage
}
