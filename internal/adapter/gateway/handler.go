package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/cronjob"
	"alfred-ai/internal/usecase/multiagent"
	"alfred-ai/internal/usecase/process"
)

// HandlerDeps holds dependencies needed by RPC handlers.
type HandlerDeps struct {
	Router         *usecase.Router
	Sessions       *usecase.SessionManager
	Tools          domain.ToolExecutor
	Memory         domain.MemoryProvider
	Plugins        domain.PluginManager    // can be nil
	Registry       *multiagent.Registry    // can be nil (single-agent mode)
	Bus            domain.EventBus
	Logger         *slog.Logger
	NodeManager    domain.NodeManager      // can be nil
	NodeTokens     domain.NodeTokenManager // can be nil
	CronManager    *cronjob.Manager        // can be nil
	ProcessManager *process.Manager        // can be nil
	ActiveRequests *sync.Map               // sessionID -> context.CancelFunc; can be nil
	Authorizer     domain.Authorizer       // can be nil (RBAC disabled)
	AuditLogger    domain.AuditLogger      // can be nil
	TenantManager  *usecase.TenantManager  // can be nil (single-tenant mode)
	GDPRHandler    *security.GDPRHandler  // can be nil
}

// requirePerm wraps an RPCHandler with RBAC enforcement.
// If deps.Authorizer is nil, the handler runs without permission checks.
// Tokens without roles are treated as admin for backward compatibility.
func requirePerm(deps HandlerDeps, perm domain.Permission, handler RPCHandler) RPCHandler {
	return func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		if deps.Authorizer != nil {
			roles := domain.StringsToAuthRoles(client.Roles)
			// Backward compat: tokens with no roles are treated as admin.
			if len(roles) == 0 {
				roles = []domain.AuthRole{domain.AuthRoleAdmin}
			}
			if err := deps.Authorizer.Authorize(ctx, roles, perm); err != nil {
				if deps.AuditLogger != nil {
					_ = deps.AuditLogger.Log(ctx, domain.AuditEvent{
						Timestamp: time.Now(),
						Type:      domain.AuditAccessDenied,
						Actor:     client.Name,
						Resource:  string(perm),
						Action:    "rpc_call",
						Outcome:   "denied",
						Detail: map[string]string{
							"roles":      fmt.Sprintf("%v", client.Roles),
							"permission": string(perm),
						},
					})
				}
				return nil, domain.ErrForbidden
			}
		}
		return handler(ctx, client, payload)
	}
}

// RegisterRESTHandlers registers HTTP REST endpoints on the gateway server.
// channelNames is the list of active channel names for the status response.
func RegisterRESTHandlers(s *Server, deps HandlerDeps, channelNames []string) *Metrics {
	startTime := time.Now()
	metrics := &Metrics{}

	// Subscribe to events for metric counters.
	if deps.Bus != nil {
		deps.Bus.Subscribe(domain.EventToolCallCompleted, func(_ context.Context, e domain.Event) {
			metrics.ToolCallsTotal.Add(1)
		})
		deps.Bus.Subscribe(domain.EventLLMCallCompleted, func(_ context.Context, e domain.Event) {
			metrics.LLMCallsTotal.Add(1)
		})
		deps.Bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, e domain.Event) {
			metrics.MessagesRecv.Add(1)
		})
		deps.Bus.Subscribe(domain.EventMessageSent, func(_ context.Context, e domain.Event) {
			metrics.MessagesSent.Add(1)
		})
		deps.Bus.Subscribe(domain.EventSessionCreated, func(_ context.Context, e domain.Event) {
			metrics.SessionsTotal.Add(1)
		})
		deps.Bus.Subscribe(domain.EventAgentError, func(_ context.Context, e domain.Event) {
			metrics.ToolErrorsTotal.Add(1)
		})
	}

	// Auth middleware for REST endpoints.
	authMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.URL.Query().Get("token")
			if token == "" {
				token = r.Header.Get("Authorization")
				if len(token) > 7 && token[:7] == "Bearer " {
					token = token[7:]
				}
			}
			if _, err := s.auth.Authenticate(token); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	s.RegisterHTTPRoute("/api/v1/status", authMiddleware(statusHandler(deps, startTime, metrics, channelNames)))
	s.RegisterHTTPRoute("/metrics", authMiddleware(metricsHandler(deps, startTime, metrics)))

	return metrics
}

// RegisterDefaultHandlers registers all built-in RPC handlers on the server.
// When deps.Authorizer is non-nil, each handler is wrapped with RBAC enforcement.
func RegisterDefaultHandlers(s *Server, deps HandlerDeps) {
	// Helper that applies requirePerm if Authorizer is set.
	rpc := func(method string, perm domain.Permission, h RPCHandler) {
		s.RegisterHandler(method, requirePerm(deps, perm, h))
	}

	rpc("chat.send", domain.PermToolExecute, chatSendHandler(deps))
	rpc("chat.stream", domain.PermToolExecute, chatStreamHandler(deps))
	rpc("chat.abort", domain.PermToolExecute, chatAbortHandler(deps))
	rpc("session.list", domain.PermSessionView, sessionListHandler(deps))
	rpc("session.get", domain.PermSessionView, sessionGetHandler(deps))
	rpc("session.delete", domain.PermSessionDelete, sessionDeleteHandler(deps))
	rpc("tool.list", domain.PermSessionView, toolListHandler(deps))
	rpc("tool.approve", domain.PermToolExecute, toolApproveHandler(deps))
	rpc("tool.deny", domain.PermToolExecute, toolDenyHandler(deps))
	rpc("memory.query", domain.PermMemoryRead, memoryQueryHandler(deps))
	rpc("memory.store", domain.PermMemoryWrite, memoryStoreHandler(deps))
	rpc("memory.delete", domain.PermMemoryDelete, memoryDeleteHandler(deps))
	rpc("config.get", domain.PermDashboard, configGetHandler(deps))

	if deps.Plugins != nil {
		rpc("plugin.list", domain.PermPluginManage, pluginListHandler(deps))
	}
	if deps.Registry != nil {
		rpc("agent.list", domain.PermSessionView, agentListHandler(deps))
	}
	if deps.NodeManager != nil {
		rpc("node.list", domain.PermNodeManage, nodeListHandler(deps))
		rpc("node.get", domain.PermNodeManage, nodeGetHandler(deps))
		rpc("node.invoke", domain.PermNodeManage, nodeInvokeHandler(deps))
		rpc("node.discover", domain.PermNodeManage, nodeDiscoverHandler(deps))
	}
	if deps.NodeTokens != nil {
		rpc("node.token.generate", domain.PermNodeManage, nodeTokenGenerateHandler(deps))
		rpc("node.token.revoke", domain.PermNodeManage, nodeTokenRevokeHandler(deps))
	}
	if deps.CronManager != nil {
		rpc("cron.list", domain.PermCronManage, cronListHandler(deps))
		rpc("cron.get", domain.PermCronManage, cronGetHandler(deps))
		rpc("cron.create", domain.PermCronManage, cronCreateHandler(deps))
		rpc("cron.update", domain.PermCronManage, cronUpdateHandler(deps))
		rpc("cron.delete", domain.PermCronManage, cronDeleteHandler(deps))
		rpc("cron.runs", domain.PermCronManage, cronRunsHandler(deps))
	}
	if deps.ProcessManager != nil {
		rpc("process.list", domain.PermProcessManage, processListHandler(deps))
		rpc("process.poll", domain.PermProcessManage, processPollHandler(deps))
		rpc("process.log", domain.PermProcessManage, processLogHandler(deps))
		rpc("process.write", domain.PermProcessManage, processWriteHandler(deps))
		rpc("process.kill", domain.PermProcessManage, processKillHandler(deps))
		rpc("process.clear", domain.PermProcessManage, processClearHandler(deps))
		rpc("process.remove", domain.PermProcessManage, processRemoveHandler(deps))
	}
	if deps.TenantManager != nil {
		rpc("tenant.list", domain.PermTenantManage, tenantListHandler(deps))
		rpc("tenant.get", domain.PermTenantManage, tenantGetHandler(deps))
		rpc("tenant.create", domain.PermTenantManage, tenantCreateHandler(deps))
		rpc("tenant.update", domain.PermTenantManage, tenantUpdateHandler(deps))
		rpc("tenant.delete", domain.PermTenantManage, tenantDeleteHandler(deps))
	}
	if deps.GDPRHandler != nil {
		rpc("gdpr.export", domain.PermTenantManage, gdprExportHandler(deps))
		rpc("gdpr.delete", domain.PermTenantManage, gdprDeleteHandler(deps))
		rpc("gdpr.anonymize", domain.PermTenantManage, gdprAnonymizeHandler(deps))
	}
}

// --- chat ---

type chatSendRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

func chatSendHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req chatSendRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" || req.Content == "" {
			return nil, domain.ErrRPCInvalidPayload
		}

		reqCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		if deps.ActiveRequests != nil {
			deps.ActiveRequests.Store(req.SessionID, cancel)
			defer deps.ActiveRequests.Delete(req.SessionID)
		}

		out, err := deps.Router.Handle(reqCtx, domain.InboundMessage{
			SessionID:   req.SessionID,
			Content:     req.Content,
			ChannelName: "gateway",
			SenderName:  client.Name,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
}

type chatStreamResponse struct {
	Streaming bool   `json:"streaming"`
	SessionID string `json:"session_id"`
}

func chatStreamHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req chatSendRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" || req.Content == "" {
			return nil, domain.ErrRPCInvalidPayload
		}

		reqCtx, cancel := context.WithCancel(ctx)

		if deps.ActiveRequests != nil {
			deps.ActiveRequests.Store(req.SessionID, cancel)
		}

		// Launch streaming in a background goroutine. Deltas arrive as events
		// via the event bus, which the gateway already forwards to all WS clients.
		go func() {
			defer cancel()
			if deps.ActiveRequests != nil {
				defer deps.ActiveRequests.Delete(req.SessionID)
			}

			_, err := deps.Router.HandleStream(reqCtx, domain.InboundMessage{
				SessionID:   req.SessionID,
				Content:     req.Content,
				ChannelName: "gateway",
				SenderName:  client.Name,
			})
			if err != nil && deps.Bus != nil {
				errPayload, _ := json.Marshal(domain.StreamErrorPayload{Error: err.Error()})
				deps.Bus.Publish(reqCtx, domain.Event{
					Type:      domain.EventStreamError,
					SessionID: req.SessionID,
					Timestamp: time.Now(),
					Payload:   errPayload,
				})
			}
		}()

		// Return immediately — client listens for stream events.
		return json.Marshal(chatStreamResponse{
			Streaming: true,
			SessionID: req.SessionID,
		})
	}
}

type chatAbortRequest struct {
	SessionID string `json:"session_id"`
}

func chatAbortHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req chatAbortRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}

		aborted := false
		if deps.ActiveRequests != nil {
			if val, ok := deps.ActiveRequests.LoadAndDelete(req.SessionID); ok {
				if cancelFn, ok := val.(context.CancelFunc); ok {
					cancelFn()
					aborted = true
				}
			}
		}

		if aborted {
			deps.Bus.Publish(ctx, domain.Event{
				Type:      domain.EventChatAborted,
				SessionID: req.SessionID,
			})
		}

		return json.Marshal(map[string]bool{"aborted": aborted})
	}
}

// --- sessions ---

func sessionListHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, client *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		ids := deps.Sessions.ListSessionsForTenant(client.TenantID)
		return json.Marshal(ids)
	}
}

type sessionGetRequest struct {
	ID string `json:"id"`
}

func sessionGetHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req sessionGetRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		s, err := deps.Sessions.GetWithTenant(req.ID, client.TenantID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(s)
	}
}

type sessionDeleteRequest struct {
	ID string `json:"id"`
}

func sessionDeleteHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req sessionDeleteRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.Sessions.DeleteWithTenant(req.ID, client.TenantID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

// --- tools ---

func toolListHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		schemas := deps.Tools.Schemas()
		return json.Marshal(schemas)
	}
}

type toolApprovalRequest struct {
	ToolCallID string `json:"tool_call_id"`
}

func publishToolApproval(deps HandlerDeps, ctx context.Context, toolCallID string, approved bool) error {
	eventPayload, err := json.Marshal(map[string]any{
		"approved":     approved,
		"tool_call_id": toolCallID,
	})
	if err != nil {
		return err
	}
	deps.Bus.Publish(ctx, domain.Event{
		Type:    domain.EventToolApprovalResp,
		Payload: eventPayload,
	})
	return nil
}

func toolApproveHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req toolApprovalRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ToolCallID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := publishToolApproval(deps, ctx, req.ToolCallID, true); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

func toolDenyHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req toolApprovalRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ToolCallID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := publishToolApproval(deps, ctx, req.ToolCallID, false); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

// --- memory ---

type memoryQueryRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func memoryQueryHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req memoryQueryRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.Limit <= 0 {
			req.Limit = 10
		} else if req.Limit > 100 {
			req.Limit = 100
		}
		entries, err := deps.Memory.Query(ctx, req.Query, req.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(entries)
	}
}

type memoryStoreRequest struct {
	Content  string            `json:"content"`
	Tags     []string          `json:"tags"`
	Metadata map[string]string `json:"metadata"`
}

func memoryStoreHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req memoryStoreRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.Content == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		entry := domain.MemoryEntry{
			Content:  req.Content,
			Tags:     req.Tags,
			Metadata: req.Metadata,
		}
		if err := deps.Memory.Store(ctx, entry); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

type memoryDeleteRequest struct {
	ID string `json:"id"`
}

func memoryDeleteHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req memoryDeleteRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.Memory.Delete(ctx, req.ID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

// --- plugins ---

func pluginListHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		manifests := deps.Plugins.List()
		return json.Marshal(manifests)
	}
}

// --- agents ---

func agentListHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		statuses := deps.Registry.List()
		return json.Marshal(statuses)
	}
}

// --- config ---

type sanitizedConfig struct {
	Version  string `json:"version"`
	Features struct {
		Gateway      bool `json:"gateway"`
		Plugins      bool `json:"plugins"`
		MultiAgent   bool `json:"multi_agent"`
		VectorMemory bool `json:"vector_memory"`
		Scheduler    bool `json:"scheduler"`
		Nodes        bool `json:"nodes"`
		Process      bool `json:"process"`
	} `json:"features"`
}

func configGetHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		cfg := sanitizedConfig{Version: "phase-5"}
		cfg.Features.Gateway = true
		cfg.Features.Nodes = deps.NodeManager != nil
		cfg.Features.Process = deps.ProcessManager != nil
		return json.Marshal(cfg)
	}
}

// --- cron ---

func cronListHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		jobs, err := deps.CronManager.List(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(jobs)
	}
}

type cronGetRequest struct {
	ID string `json:"id"`
}

func cronGetHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req cronGetRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		job, err := deps.CronManager.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(job)
	}
}

type cronCreateRequest struct {
	Name     string              `json:"name"`
	Schedule domain.CronSchedule `json:"schedule"`
	Message  string              `json:"message"`
	AgentID  string              `json:"agent_id"`
	Channel  string              `json:"channel"`
}

func cronCreateHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req cronCreateRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		job, err := deps.CronManager.Create(ctx, domain.CronJob{
			Name:     req.Name,
			Schedule: req.Schedule,
			Action: domain.CronAction{
				Kind:    "agent_run",
				AgentID: req.AgentID,
				Channel: req.Channel,
				Message: req.Message,
			},
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(job)
	}
}

type cronUpdateRequest struct {
	ID       string               `json:"id"`
	Name     *string              `json:"name,omitempty"`
	Schedule *domain.CronSchedule `json:"schedule,omitempty"`
	Message  *string              `json:"message,omitempty"`
	Enabled  *bool                `json:"enabled,omitempty"`
}

func cronUpdateHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req cronUpdateRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		job, err := deps.CronManager.Update(ctx, req.ID, cronjob.Patch{
			Name:     req.Name,
			Schedule: req.Schedule,
			Message:  req.Message,
			Enabled:  req.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(job)
	}
}

type cronDeleteRequest struct {
	ID string `json:"id"`
}

func cronDeleteHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req cronDeleteRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.CronManager.Delete(ctx, req.ID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

type cronRunsRequest struct {
	JobID string `json:"job_id"`
	Limit int    `json:"limit"`
}

func cronRunsHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req cronRunsRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.JobID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.Limit <= 0 {
			req.Limit = 10
		}
		runs, err := deps.CronManager.ListRuns(ctx, req.JobID, req.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(runs)
	}
}

// --- process ---

func processListHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		entries := deps.ProcessManager.List("")
		return json.Marshal(entries)
	}
}

type processPollRequest struct {
	SessionID string `json:"session_id"`
}

func processPollHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req processPollRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		result, err := deps.ProcessManager.Poll(req.SessionID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
}

type processLogRequest struct {
	SessionID string `json:"session_id"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
}

func processLogHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req processLogRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.Offset < 0 {
			req.Offset = 0
		}
		if req.Limit <= 0 {
			req.Limit = process.DefaultLogLimit
		}
		result, err := deps.ProcessManager.Log(req.SessionID, req.Offset, req.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
}

type processWriteRequest struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
}

func processWriteHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req processWriteRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" || req.Input == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.ProcessManager.Write(req.SessionID, req.Input); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

type processKillRequest struct {
	SessionID string `json:"session_id"`
}

func processKillHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req processKillRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.ProcessManager.Kill(ctx, req.SessionID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"killed": true})
	}
}

func processClearHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		removed := deps.ProcessManager.Clear()
		return json.Marshal(map[string]int{"cleared": removed})
	}
}

type processRemoveRequest struct {
	SessionID string `json:"session_id"`
}

func processRemoveHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req processRemoveRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.SessionID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.ProcessManager.Remove(ctx, req.SessionID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"removed": true})
	}
}

// --- tenants ---

func tenantListHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		tenants, err := deps.TenantManager.List(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(tenants)
	}
}

type tenantGetRequest struct {
	ID string `json:"id"`
}

func tenantGetHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req tenantGetRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		t, err := deps.TenantManager.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(t)
	}
}

type tenantCreateRequest struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Plan   domain.TenantPlan  `json:"plan"`
	Config domain.TenantConfig `json:"config"`
	Limits domain.TenantLimits `json:"limits"`
}

func tenantCreateHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req tenantCreateRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		t := &domain.Tenant{
			ID:     req.ID,
			Name:   req.Name,
			Plan:   req.Plan,
			Config: req.Config,
			Limits: req.Limits,
		}
		if err := deps.TenantManager.Create(ctx, t); err != nil {
			return nil, err
		}
		if deps.AuditLogger != nil {
			_ = deps.AuditLogger.Log(ctx, domain.AuditEvent{
				Timestamp: time.Now(),
				Type:      domain.AuditTenantCreate,
				Actor:     client.Name,
				Resource:  t.ID,
				Action:    "tenant_create",
				Outcome:   "success",
			})
		}
		return json.Marshal(t)
	}
}

type tenantUpdateRequest struct {
	ID     string              `json:"id"`
	Name   *string             `json:"name,omitempty"`
	Plan   *domain.TenantPlan  `json:"plan,omitempty"`
	Config *domain.TenantConfig `json:"config,omitempty"`
	Limits *domain.TenantLimits `json:"limits,omitempty"`
}

func tenantUpdateHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req tenantUpdateRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		t, err := deps.TenantManager.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		if req.Name != nil {
			t.Name = *req.Name
		}
		if req.Plan != nil {
			t.Plan = *req.Plan
		}
		if req.Config != nil {
			t.Config = *req.Config
		}
		if req.Limits != nil {
			t.Limits = *req.Limits
		}
		if err := deps.TenantManager.Update(ctx, t); err != nil {
			return nil, err
		}
		return json.Marshal(t)
	}
}

type tenantDeleteRequest struct {
	ID string `json:"id"`
}

func tenantDeleteHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req tenantDeleteRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.TenantManager.Delete(ctx, req.ID); err != nil {
			return nil, err
		}
		if deps.AuditLogger != nil {
			_ = deps.AuditLogger.Log(ctx, domain.AuditEvent{
				Timestamp: time.Now(),
				Type:      domain.AuditTenantDelete,
				Actor:     client.Name,
				Resource:  req.ID,
				Action:    "tenant_delete",
				Outcome:   "success",
			})
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

// --- GDPR ---

type gdprRequest struct {
	UserID    string `json:"user_id"`
	OutputDir string `json:"output_dir,omitempty"`
}

func gdprExportHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req gdprRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.UserID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		outputDir := req.OutputDir
		if outputDir == "" {
			outputDir = "./data/gdpr_exports"
		}
		result, err := deps.GDPRHandler.ExportUserData(ctx, req.UserID, outputDir)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
}

func gdprDeleteHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req gdprRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.UserID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.GDPRHandler.DeleteUserData(ctx, req.UserID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}

func gdprAnonymizeHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req gdprRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.UserID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		if err := deps.GDPRHandler.AnonymizeUserData(ctx, req.UserID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	}
}
