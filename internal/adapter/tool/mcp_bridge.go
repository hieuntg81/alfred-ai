package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

// mcpCallTimeout is the default per-call timeout for MCP tool execution.
const mcpCallTimeout = 30 * time.Second

// MCPBridge manages connections to MCP servers and exposes their tools as domain.Tool instances.
type MCPBridge struct {
	servers []mcpServerConn
	tools   []domain.Tool
	logger  *slog.Logger
	mu      sync.RWMutex
}

type mcpServerConn struct {
	name   string
	client mcpClient
}

// mcpClient abstracts the MCP client interface for testability.
type mcpClient interface {
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

// NewMCPBridge creates and initializes an MCP bridge from configuration.
// It connects to all configured MCP servers, discovers their tools, and wraps them.
func NewMCPBridge(ctx context.Context, servers []config.MCPServer, logger *slog.Logger) (*MCPBridge, error) {
	b := &MCPBridge{
		logger: logger,
	}

	for _, srv := range servers {
		conn, err := b.connectServer(ctx, srv)
		if err != nil {
			// Close already-connected servers on failure.
			b.Close()
			return nil, fmt.Errorf("mcp server %q: %w", srv.Name, err)
		}
		b.servers = append(b.servers, *conn)
	}

	if err := b.discoverTools(ctx); err != nil {
		b.Close()
		return nil, fmt.Errorf("discover tools: %w", err)
	}

	return b, nil
}

// newMCPBridgeWithClients creates an MCPBridge with pre-built clients (for testing).
func newMCPBridgeWithClients(ctx context.Context, servers []mcpServerConn, logger *slog.Logger) (*MCPBridge, error) {
	b := &MCPBridge{
		servers: servers,
		logger:  logger,
	}
	if err := b.discoverTools(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *MCPBridge) connectServer(ctx context.Context, srv config.MCPServer) (*mcpServerConn, error) {
	var c mcpClient
	var err error

	switch srv.Transport {
	case "stdio":
		env := envSlice(srv.Env)
		c, err = mcpclient.NewStdioMCPClient(srv.Command, env, srv.Args...)
		if err != nil {
			return nil, fmt.Errorf("create stdio client: %w", err)
		}
	case "http":
		t, tErr := transport.NewStreamableHTTP(srv.URL)
		if tErr != nil {
			return nil, fmt.Errorf("create http transport: %w", tErr)
		}
		httpClient := mcpclient.NewClient(t)
		if err = httpClient.Start(ctx); err != nil {
			return nil, fmt.Errorf("start http client: %w", err)
		}
		c = httpClient
	default:
		return nil, fmt.Errorf("unsupported transport %q", srv.Transport)
	}

	// Initialize the MCP connection.
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "alfredai",
		Version: "1.0.0",
	}

	if ic, ok := c.(interface {
		Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	}); ok {
		if _, err = ic.Initialize(ctx, initReq); err != nil {
			c.Close()
			return nil, domain.WrapOp("initialize", err)
		}
	}

	b.logger.Info("mcp server connected", "name", srv.Name, "transport", srv.Transport)

	return &mcpServerConn{
		name:   srv.Name,
		client: c,
	}, nil
}

func (b *MCPBridge) discoverTools(ctx context.Context) error {
	var errs []string
	successCount := 0

	for _, srv := range b.servers {
		result, err := srv.client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			b.logger.Warn("mcp server discovery failed, skipping",
				"server", srv.name,
				"error", err,
			)
			errs = append(errs, fmt.Sprintf("%s: %v", srv.name, err))
			continue
		}

		for _, t := range result.Tools {
			adapter := newMCPToolAdapter(srv.name, srv.client, t, b.logger)
			b.tools = append(b.tools, adapter)
			b.logger.Debug("mcp tool discovered",
				"server", srv.name,
				"tool", t.Name,
				"full_name", adapter.Name())
		}

		b.logger.Info("mcp tools discovered", "server", srv.name, "count", len(result.Tools))
		successCount++
	}

	// Only fail if ALL servers failed.
	if successCount == 0 && len(errs) > 0 {
		return fmt.Errorf("all mcp servers failed discovery: %s", strings.Join(errs, "; "))
	}

	return nil
}

// Tools returns all discovered MCP tools as domain.Tool instances.
func (b *MCPBridge) Tools() []domain.Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.tools
}

// Close shuts down all MCP server connections.
func (b *MCPBridge) Close() {
	for _, srv := range b.servers {
		if err := srv.client.Close(); err != nil {
			b.logger.Warn("mcp server close error", "server", srv.name, "error", err)
		}
	}
}

// --- MCP Tool Adapter ---

// mcpToolAdapter wraps a single MCP tool as a domain.Tool.
type mcpToolAdapter struct {
	serverName string
	client     mcpClient
	mcpTool    mcp.Tool
	fullName   string
	logger     *slog.Logger
}

func newMCPToolAdapter(serverName string, client mcpClient, t mcp.Tool, logger *slog.Logger) *mcpToolAdapter {
	return &mcpToolAdapter{
		serverName: serverName,
		client:     client,
		mcpTool:    t,
		fullName:   fmt.Sprintf("mcp_%s_%s", sanitizeName(serverName), sanitizeName(t.Name)),
		logger:     logger,
	}
}

func (a *mcpToolAdapter) Name() string {
	return a.fullName
}

func (a *mcpToolAdapter) Description() string {
	desc := a.mcpTool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %q from server %q", a.mcpTool.Name, a.serverName)
	}
	return desc
}

func (a *mcpToolAdapter) Schema() domain.ToolSchema {
	// Convert MCP tool's input schema to JSON for domain.ToolSchema.Parameters.
	params := json.RawMessage(`{"type": "object"}`)
	if a.mcpTool.InputSchema.Properties != nil || a.mcpTool.InputSchema.Required != nil {
		if data, err := json.Marshal(a.mcpTool.InputSchema); err == nil {
			params = data
		}
	}

	return domain.ToolSchema{
		Name:        a.fullName,
		Description: a.Description(),
		Parameters:  params,
	}
}

func (a *mcpToolAdapter) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	// Parse arguments from raw JSON.
	var args map[string]interface{}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &args); err != nil {
			return &domain.ToolResult{
				Content: fmt.Sprintf("invalid arguments: %v", err),
				IsError: true,
			}, nil
		}
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = a.mcpTool.Name
	callReq.Params.Arguments = args

	a.logger.Debug("mcp tool call",
		"server", a.serverName,
		"tool", a.mcpTool.Name,
		"full_name", a.fullName)

	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	result, err := a.client.CallTool(callCtx, callReq)
	if err != nil {
		return &domain.ToolResult{
			Content:     fmt.Sprintf("MCP tool error: %v", err),
			IsError:     true,
			IsRetryable: true,
		}, nil
	}

	// Extract text content from MCP result.
	content := extractMCPContent(result)

	return &domain.ToolResult{
		Content: content,
		IsError: result.IsError,
	}, nil
}

// extractMCPContent converts MCP CallToolResult content to a string.
func extractMCPContent(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		switch v := c.(type) {
		case mcp.TextContent:
			parts = append(parts, v.Text)
		case *mcp.TextContent:
			parts = append(parts, v.Text)
		default:
			// For non-text content, marshal to JSON.
			if data, err := json.Marshal(v); err == nil {
				parts = append(parts, string(data))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// --- Helpers ---

// sanitizeName replaces characters that aren't valid in tool names.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// envSlice converts a map of env vars to KEY=VALUE slices.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
