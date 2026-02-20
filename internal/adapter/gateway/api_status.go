package gateway

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// StatusResponse is the JSON body returned by GET /api/v1/status.
type StatusResponse struct {
	Agent    AgentStatus    `json:"agent"`
	Sessions SessionStatus  `json:"sessions"`
	Tools    ToolStatus     `json:"tools"`
	Memory   MemoryStatus   `json:"memory"`
	Channels []string       `json:"channels"`
}

// AgentStatus holds agent overview info.
type AgentStatus struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// SessionStatus holds session counts.
type SessionStatus struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// ToolStatus holds tool usage stats.
type ToolStatus struct {
	Registered int   `json:"registered"`
	CallsTotal int64 `json:"calls_total"`
	ErrorsTotal int64 `json:"errors_total"`
}

// MemoryStatus holds memory system info.
type MemoryStatus struct {
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
}

// Metrics tracks counters for the status API and Prometheus metrics.
type Metrics struct {
	ToolCallsTotal  atomic.Int64
	ToolErrorsTotal atomic.Int64
	LLMCallsTotal   atomic.Int64
	MessagesRecv    atomic.Int64
	MessagesSent    atomic.Int64
	SessionsTotal   atomic.Int64
}

// statusHandler returns an HTTP handler for GET /api/v1/status.
func statusHandler(deps HandlerDeps, startTime time.Time, metrics *Metrics, channelNames []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionIDs := deps.Sessions.ListSessions()
		toolSchemas := deps.Tools.Schemas()

		resp := StatusResponse{
			Agent: AgentStatus{
				Name:          "alfred-ai",
				Version:       "phase-3",
				UptimeSeconds: int64(time.Since(startTime).Seconds()),
			},
			Sessions: SessionStatus{
				Active: len(sessionIDs),
				Total:  len(sessionIDs),
			},
			Tools: ToolStatus{
				Registered:  len(toolSchemas),
				CallsTotal:  metrics.ToolCallsTotal.Load(),
				ErrorsTotal: metrics.ToolErrorsTotal.Load(),
			},
			Memory: MemoryStatus{
				Provider:  deps.Memory.Name(),
				Available: deps.Memory.IsAvailable(),
			},
			Channels: channelNames,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
