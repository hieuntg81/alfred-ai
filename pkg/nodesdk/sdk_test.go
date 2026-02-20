package nodesdk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewDefaults(t *testing.T) {
	n := New("node-1", "Test Node")
	if n.ID() != "node-1" {
		t.Errorf("ID() = %q", n.ID())
	}
	if n.Name() != "Test Node" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.platform != "unknown" {
		t.Errorf("platform = %q", n.platform)
	}
}

func TestWithOptions(t *testing.T) {
	n := New("node-1", "Test",
		WithServer("localhost:9090"),
		WithToken("secret"),
		WithPlatform("linux/arm64"),
		WithLogger(testLogger()),
		WithListenPort(8080),
	)
	if n.serverAddr != "localhost:9090" {
		t.Errorf("serverAddr = %q", n.serverAddr)
	}
	if n.deviceToken != "secret" {
		t.Errorf("deviceToken = %q", n.deviceToken)
	}
	if n.platform != "linux/arm64" {
		t.Errorf("platform = %q", n.platform)
	}
	if n.listenPort != 8080 {
		t.Errorf("listenPort = %d", n.listenPort)
	}
}

func TestRegisterCapability(t *testing.T) {
	n := New("node-1", "Test", WithLogger(testLogger()))
	n.RegisterCapability("echo", "Echo params back", nil,
		func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			return params, nil
		},
	)

	caps := n.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("capabilities len = %d, want 1", len(caps))
	}
	if caps[0].Name != "echo" {
		t.Errorf("capability name = %q", caps[0].Name)
	}
}

func TestHandleInvocation(t *testing.T) {
	n := New("node-1", "Test", WithLogger(testLogger()))
	n.RegisterCapability("add", "Add numbers", nil,
		func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"sum":42}`), nil
		},
	)

	result, err := n.HandleInvocation(context.Background(), "add", nil)
	if err != nil {
		t.Fatalf("HandleInvocation: %v", err)
	}
	if string(result) != `{"sum":42}` {
		t.Errorf("result = %s", result)
	}
}

func TestUnknownCapability(t *testing.T) {
	n := New("node-1", "Test", WithLogger(testLogger()))
	_, err := n.HandleInvocation(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestCapabilitiesAll(t *testing.T) {
	n := New("node-1", "Test", WithLogger(testLogger()))
	n.RegisterCapability("cap1", "First", nil, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) { return nil, nil })
	n.RegisterCapability("cap2", "Second", nil, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) { return nil, nil })

	caps := n.Capabilities()
	if len(caps) != 2 {
		t.Errorf("capabilities len = %d, want 2", len(caps))
	}

	// Verify handler is not exposed in returned capabilities.
	for _, c := range caps {
		if c.handler != nil {
			t.Error("handler should not be exposed in Capabilities()")
		}
	}
}
