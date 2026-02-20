package gateway

import (
	"fmt"
	"net/http"
	"runtime"
	"time"
)

// metricsHandler returns an HTTP handler for GET /metrics in Prometheus text format.
// This uses the lightweight text format to avoid pulling in the full prometheus client.
func metricsHandler(deps HandlerDeps, startTime time.Time, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		sessionIDs := deps.Sessions.ListSessions()
		toolSchemas := deps.Tools.Schemas()

		// Session metrics.
		fmt.Fprintf(w, "# HELP alfredai_sessions_active Number of active sessions.\n")
		fmt.Fprintf(w, "# TYPE alfredai_sessions_active gauge\n")
		fmt.Fprintf(w, "alfredai_sessions_active %d\n", len(sessionIDs))

		fmt.Fprintf(w, "# HELP alfredai_sessions_total Total number of sessions created.\n")
		fmt.Fprintf(w, "# TYPE alfredai_sessions_total counter\n")
		fmt.Fprintf(w, "alfredai_sessions_total %d\n", metrics.SessionsTotal.Load())

		// Tool metrics.
		fmt.Fprintf(w, "# HELP alfredai_tool_calls_total Total tool invocations.\n")
		fmt.Fprintf(w, "# TYPE alfredai_tool_calls_total counter\n")
		fmt.Fprintf(w, "alfredai_tool_calls_total %d\n", metrics.ToolCallsTotal.Load())

		fmt.Fprintf(w, "# HELP alfredai_tool_errors_total Total tool errors.\n")
		fmt.Fprintf(w, "# TYPE alfredai_tool_errors_total counter\n")
		fmt.Fprintf(w, "alfredai_tool_errors_total %d\n", metrics.ToolErrorsTotal.Load())

		fmt.Fprintf(w, "# HELP alfredai_tools_registered Number of registered tools.\n")
		fmt.Fprintf(w, "# TYPE alfredai_tools_registered gauge\n")
		fmt.Fprintf(w, "alfredai_tools_registered %d\n", len(toolSchemas))

		// LLM metrics.
		fmt.Fprintf(w, "# HELP alfredai_llm_calls_total Total LLM calls.\n")
		fmt.Fprintf(w, "# TYPE alfredai_llm_calls_total counter\n")
		fmt.Fprintf(w, "alfredai_llm_calls_total %d\n", metrics.LLMCallsTotal.Load())

		// Message metrics.
		fmt.Fprintf(w, "# HELP alfredai_messages_received_total Total messages received.\n")
		fmt.Fprintf(w, "# TYPE alfredai_messages_received_total counter\n")
		fmt.Fprintf(w, "alfredai_messages_received_total %d\n", metrics.MessagesRecv.Load())

		fmt.Fprintf(w, "# HELP alfredai_messages_sent_total Total messages sent.\n")
		fmt.Fprintf(w, "# TYPE alfredai_messages_sent_total counter\n")
		fmt.Fprintf(w, "alfredai_messages_sent_total %d\n", metrics.MessagesSent.Load())

		// Memory metrics (availability).
		available := 0
		if deps.Memory.IsAvailable() {
			available = 1
		}
		fmt.Fprintf(w, "# HELP alfredai_memory_available Whether memory provider is available.\n")
		fmt.Fprintf(w, "# TYPE alfredai_memory_available gauge\n")
		fmt.Fprintf(w, "alfredai_memory_available %d\n", available)

		// Uptime.
		fmt.Fprintf(w, "# HELP alfredai_uptime_seconds Seconds since agent started.\n")
		fmt.Fprintf(w, "# TYPE alfredai_uptime_seconds gauge\n")
		fmt.Fprintf(w, "alfredai_uptime_seconds %.0f\n", time.Since(startTime).Seconds())

		// Go runtime metrics.
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines.\n")
		fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
		fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())

		fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Bytes of allocated heap objects.\n")
		fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n", mem.Alloc)

		fmt.Fprintf(w, "# HELP go_memstats_sys_bytes Total bytes of memory obtained from the OS.\n")
		fmt.Fprintf(w, "# TYPE go_memstats_sys_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_sys_bytes %d\n", mem.Sys)

		fmt.Fprintf(w, "# HELP go_gc_duration_seconds Total GC pause duration.\n")
		fmt.Fprintf(w, "# TYPE go_gc_duration_seconds gauge\n")
		fmt.Fprintf(w, "go_gc_duration_seconds %f\n", float64(mem.PauseTotalNs)/1e9)
	}
}
