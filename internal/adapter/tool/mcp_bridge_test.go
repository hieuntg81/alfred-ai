package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"alfred-ai/internal/infra/config"
)

// mockMCPClient implements mcpClient for testing.
type mockMCPClient struct {
	tools    []mcp.Tool
	callFunc func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
	closed   bool
	listErr  error
}

func (m *mockMCPClient) ListTools(_ context.Context, _ mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &mcp.ListToolsResult{
		Tools: m.tools,
	}, nil
}

func (m *mockMCPClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.callFunc != nil {
		return m.callFunc(ctx, req)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(fmt.Sprintf("called %s", req.Params.Name)),
		},
	}, nil
}

func (m *mockMCPClient) Close() error {
	m.closed = true
	return nil
}

func mcpTestLogger() *slog.Logger { return slog.Default() }

func TestMCPBridgeDiscoverTools(t *testing.T) {
	mock := &mockMCPClient{
		tools: []mcp.Tool{
			{Name: "read_file", Description: "Read a file"},
			{Name: "write_file", Description: "Write a file"},
		},
	}

	bridge, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "filesystem", client: mock},
	}, mcpTestLogger())
	if err != nil {
		t.Fatalf("NewMCPBridge: %v", err)
	}
	defer bridge.Close()

	tools := bridge.Tools()
	if len(tools) != 2 {
		t.Fatalf("Tools count = %d, want 2", len(tools))
	}

	if tools[0].Name() != "mcp_filesystem_read_file" {
		t.Errorf("tools[0].Name = %q", tools[0].Name())
	}
	if tools[1].Name() != "mcp_filesystem_write_file" {
		t.Errorf("tools[1].Name = %q", tools[1].Name())
	}
}

func TestMCPBridgeMultipleServers(t *testing.T) {
	mock1 := &mockMCPClient{
		tools: []mcp.Tool{
			{Name: "search", Description: "Search things"},
		},
	}
	mock2 := &mockMCPClient{
		tools: []mcp.Tool{
			{Name: "query", Description: "Query database"},
			{Name: "insert", Description: "Insert record"},
		},
	}

	bridge, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "search", client: mock1},
		{name: "database", client: mock2},
	}, mcpTestLogger())
	if err != nil {
		t.Fatalf("NewMCPBridge: %v", err)
	}
	defer bridge.Close()

	tools := bridge.Tools()
	if len(tools) != 3 {
		t.Fatalf("Tools count = %d, want 3", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	expected := []string{"mcp_search_search", "mcp_database_query", "mcp_database_insert"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestMCPBridgeListToolsError(t *testing.T) {
	mock := &mockMCPClient{
		listErr: fmt.Errorf("connection refused"),
	}

	_, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "bad-server", client: mock},
	}, mcpTestLogger())
	if err == nil {
		t.Error("expected error when ALL servers fail discovery")
	}
}

func TestMCPBridgePartialDiscoveryFailure(t *testing.T) {
	// Server 1 succeeds, server 2 fails → bridge should work with server 1's tools.
	mockOK := &mockMCPClient{
		tools: []mcp.Tool{
			{Name: "search", Description: "Search things"},
		},
	}
	mockFail := &mockMCPClient{
		listErr: fmt.Errorf("connection refused"),
	}

	bridge, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "ok-server", client: mockOK},
		{name: "bad-server", client: mockFail},
	}, mcpTestLogger())
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}
	defer bridge.Close()

	tools := bridge.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool from successful server, got %d", len(tools))
	}
	if tools[0].Name() != "mcp_ok_server_search" {
		t.Errorf("tool name = %q, want mcp_ok_server_search", tools[0].Name())
	}
}

func TestMCPBridgeAllServersFailDiscovery(t *testing.T) {
	mock1 := &mockMCPClient{listErr: fmt.Errorf("error 1")}
	mock2 := &mockMCPClient{listErr: fmt.Errorf("error 2")}

	_, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "bad1", client: mock1},
		{name: "bad2", client: mock2},
	}, mcpTestLogger())
	if err == nil {
		t.Fatal("expected error when all servers fail")
	}
	if !strings.Contains(err.Error(), "all mcp servers failed") {
		t.Errorf("error = %q, want to contain 'all mcp servers failed'", err.Error())
	}
}

func TestMCPToolAdapterExecuteTimeout(t *testing.T) {
	// Verify that the execute method uses a timeout context.
	mock := &mockMCPClient{
		callFunc: func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Check that the context has a deadline set.
			if _, ok := ctx.Deadline(); !ok {
				t.Error("expected context with deadline (timeout)")
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("ok"),
				},
			}, nil
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "timed"}, mcpTestLogger())
	result, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("IsError = true: %s", result.Content)
	}
}

func TestMCPBridgeClose(t *testing.T) {
	mock1 := &mockMCPClient{tools: []mcp.Tool{}}
	mock2 := &mockMCPClient{tools: []mcp.Tool{}}

	bridge, err := newMCPBridgeWithClients(context.Background(), []mcpServerConn{
		{name: "srv1", client: mock1},
		{name: "srv2", client: mock2},
	}, mcpTestLogger())
	if err != nil {
		t.Fatalf("NewMCPBridge: %v", err)
	}

	bridge.Close()

	if !mock1.closed {
		t.Error("srv1 should be closed")
	}
	if !mock2.closed {
		t.Error("srv2 should be closed")
	}
}

func TestMCPToolAdapterName(t *testing.T) {
	adapter := newMCPToolAdapter("my-server", nil, mcp.Tool{Name: "my-tool"}, mcpTestLogger())
	if adapter.Name() != "mcp_my_server_my_tool" {
		t.Errorf("Name = %q, want mcp_my_server_my_tool", adapter.Name())
	}
}

func TestMCPToolAdapterDescription(t *testing.T) {
	adapter := newMCPToolAdapter("srv", nil, mcp.Tool{
		Name:        "do_stuff",
		Description: "Does stuff",
	}, mcpTestLogger())
	if adapter.Description() != "Does stuff" {
		t.Errorf("Description = %q", adapter.Description())
	}

	// Empty description should generate one.
	adapter2 := newMCPToolAdapter("srv", nil, mcp.Tool{Name: "do_stuff"}, mcpTestLogger())
	if adapter2.Description() == "" {
		t.Error("Description should not be empty for tool without description")
	}
}

func TestMCPToolAdapterSchema(t *testing.T) {
	mcpTool := mcp.Tool{
		Name:        "greet",
		Description: "Greet someone",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name to greet",
				},
			},
			Required: []string{"name"},
		},
	}

	adapter := newMCPToolAdapter("test", nil, mcpTool, mcpTestLogger())
	schema := adapter.Schema()

	if schema.Name != "mcp_test_greet" {
		t.Errorf("Schema.Name = %q", schema.Name)
	}
	if schema.Description != "Greet someone" {
		t.Errorf("Schema.Description = %q", schema.Description)
	}

	// Parameters should be valid JSON with properties.
	var params map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if params["type"] != "object" {
		t.Errorf("params.type = %v", params["type"])
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("params.properties not a map")
	}
	if _, ok := props["name"]; !ok {
		t.Error("params.properties missing 'name'")
	}
}

func TestMCPToolAdapterSchemaEmpty(t *testing.T) {
	mcpTool := mcp.Tool{
		Name: "no_params",
	}
	adapter := newMCPToolAdapter("test", nil, mcpTool, mcpTestLogger())
	schema := adapter.Schema()

	var params map[string]interface{}
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if params["type"] != "object" {
		t.Errorf("params.type = %v, want object", params["type"])
	}
}

func TestMCPToolAdapterExecute(t *testing.T) {
	mock := &mockMCPClient{
		callFunc: func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, ok := req.Params.Arguments.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("expected map arguments, got %T", req.Params.Arguments)
			}
			name := args["name"]
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Hello, %s!", name)),
				},
			}, nil
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "greet"}, mcpTestLogger())

	params := json.RawMessage(`{"name": "World"}`)
	result, err := adapter.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Errorf("IsError = true, content: %s", result.Content)
	}
	if result.Content != "Hello, World!" {
		t.Errorf("Content = %q, want 'Hello, World!'", result.Content)
	}
}

func TestMCPToolAdapterExecuteError(t *testing.T) {
	mock := &mockMCPClient{
		callFunc: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, fmt.Errorf("server unavailable")
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "broken"}, mcpTestLogger())

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for MCP call failure")
	}
	if !result.IsRetryable {
		t.Error("IsRetryable should be true for MCP call failure")
	}
}

func TestMCPToolAdapterExecuteToolError(t *testing.T) {
	mock := &mockMCPClient{
		callFunc: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("file not found"),
				},
				IsError: true,
			}, nil
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "read"}, mcpTestLogger())

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{"path": "/nonexistent"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true when MCP tool returns error")
	}
	if result.Content != "file not found" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestMCPToolAdapterExecuteInvalidParams(t *testing.T) {
	adapter := newMCPToolAdapter("test", nil, mcp.Tool{Name: "test"}, mcpTestLogger())

	result, err := adapter.Execute(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("IsError should be true for invalid params")
	}
}

func TestMCPToolAdapterExecuteNullParams(t *testing.T) {
	mock := &mockMCPClient{
		callFunc: func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("ok"),
				},
			}, nil
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "no_args"}, mcpTestLogger())

	// Both null and empty params should work.
	for _, params := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("")} {
		result, err := adapter.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute(%s): %v", string(params), err)
		}
		if result.IsError {
			t.Errorf("Execute(%s): IsError = true: %s", string(params), result.Content)
		}
	}
}

func TestMCPToolAdapterExecuteMultiContent(t *testing.T) {
	mock := &mockMCPClient{
		callFunc: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("line 1"),
					mcp.NewTextContent("line 2"),
				},
			}, nil
		},
	}

	adapter := newMCPToolAdapter("test", mock, mcp.Tool{Name: "multi"}, mcpTestLogger())

	result, err := adapter.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "line 1\nline 2" {
		t.Errorf("Content = %q, want 'line 1\\nline 2'", result.Content)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with-dash", "with_dash"},
		{"with.dot", "with_dot"},
		{"with spaces", "with_spaces"},
		{"CamelCase", "CamelCase"},
		{"under_score", "under_score"},
		{"123numbers", "123numbers"},
		{"special!@#$%", "special_____"},
	}

	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnvSlice(t *testing.T) {
	result := envSlice(nil)
	if result != nil {
		t.Errorf("envSlice(nil) = %v, want nil", result)
	}

	result = envSlice(map[string]string{
		"KEY1": "val1",
		"KEY2": "val2",
	})
	if len(result) != 2 {
		t.Fatalf("envSlice len = %d, want 2", len(result))
	}
	found := make(map[string]bool)
	for _, v := range result {
		found[v] = true
	}
	if !found["KEY1=val1"] || !found["KEY2=val2"] {
		t.Errorf("envSlice = %v", result)
	}
}

func TestMCPValidation(t *testing.T) {
	tests := []struct {
		name    string
		servers []config.MCPServer
	}{
		{
			name: "valid stdio",
			servers: []config.MCPServer{
				{Name: "test", Transport: "stdio", Command: "echo"},
			},
		},
		{
			name: "valid http",
			servers: []config.MCPServer{
				{Name: "test", Transport: "http", URL: "http://localhost:8080"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, s := range tt.servers {
				if s.Name == "" {
					t.Error("server name should not be empty")
				}
			}
		})
	}
}

func TestExtractMCPContentEmpty(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{},
	}
	content := extractMCPContent(result)
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}
