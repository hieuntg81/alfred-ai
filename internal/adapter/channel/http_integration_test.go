//go:build integration
// +build integration

package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func TestHTTPChannel_RealRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start HTTP channel
	ch := NewHTTPChannel("127.0.0.1:0", nil)

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		// Echo handler for testing
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   "Echo: " + msg.Content,
		})
	}

	if err := ch.Start(ctx, handler); err != nil {
		t.Fatalf("Failed to start HTTP channel: %v", err)
	}
	defer ch.Stop(ctx)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Make real HTTP request
	reqBody := map[string]string{
		"session_id": "test-session",
		"content":    "Hello integration test",
	}
	body, _ := json.Marshal(reqBody)

	addr := ch.boundAddr
	if addr == "" {
		t.Fatal("HTTP channel not bound")
	}

	resp, err := http.Post(
		fmt.Sprintf("http://%s/api/v1/chat", addr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expected := "Echo: Hello integration test"
	if chatResp.Content != expected {
		t.Errorf("Expected %q, got %q", expected, chatResp.Content)
	}

	t.Logf("HTTP integration test passed. Response: %s", chatResp.Content)
}

func TestHTTPChannel_RealRequest_MultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch := NewHTTPChannel("127.0.0.1:0", nil)

	messageCount := 0
	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		messageCount++
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   fmt.Sprintf("Message %d received", messageCount),
		})
	}

	if err := ch.Start(ctx, handler); err != nil {
		t.Fatalf("Failed to start HTTP channel: %v", err)
	}
	defer ch.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Send multiple requests
	for i := 1; i <= 3; i++ {
		reqBody := map[string]string{
			"session_id": "multi-test",
			"content":    fmt.Sprintf("Message %d", i),
		}
		body, _ := json.Marshal(reqBody)

		resp, err := http.Post(
			fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}

		var chatResp chatResponse
		json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()

		expected := fmt.Sprintf("Message %d received", i)
		if chatResp.Content != expected {
			t.Errorf("Request %d: expected %q, got %q", i, expected, chatResp.Content)
		}

		t.Logf("Request %d: %s", i, chatResp.Content)
	}
}

func TestHTTPChannel_RealRequest_HealthCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch := NewHTTPChannel("127.0.0.1:0", nil)

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return nil
	}

	if err := ch.Start(ctx, handler); err != nil {
		t.Fatalf("Failed to start HTTP channel: %v", err)
	}
	defer ch.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/health", ch.boundAddr))
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health check: expected 200, got %d", resp.StatusCode)
	}

	var healthResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if healthResp["status"] != "ok" {
		t.Errorf("Health status: expected 'ok', got %q", healthResp["status"])
	}

	t.Logf("Health check passed: %+v", healthResp)
}

func TestHTTPChannel_RealRequest_InvalidRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch := NewHTTPChannel("127.0.0.1:0", nil)

	handler := func(ctx context.Context, msg domain.InboundMessage) error {
		return ch.Send(ctx, domain.OutboundMessage{
			SessionID: msg.SessionID,
			Content:   "OK",
		})
	}

	if err := ch.Start(ctx, handler); err != nil {
		t.Fatalf("Failed to start HTTP channel: %v", err)
	}
	defer ch.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Test invalid JSON
	resp, err := http.Post(
		fmt.Sprintf("http://%s/api/v1/chat", ch.boundAddr),
		"application/json",
		bytes.NewReader([]byte("invalid json")),
	)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Invalid JSON: expected 400, got %d", resp.StatusCode)
	}

	t.Logf("Invalid request handling passed")
}
