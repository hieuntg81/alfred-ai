package tool

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func FuzzBrowserToolExecute(f *testing.F) {
	f.Add([]byte(`{"action":"navigate","url":"https://example.com"}`))
	f.Add([]byte(`{"action":"get_content"}`))
	f.Add([]byte(`{"action":"screenshot","full_page":true}`))
	f.Add([]byte(`{"action":"click","selector":"#btn"}`))
	f.Add([]byte(`{"action":"type","selector":"#input","text":"hello"}`))
	f.Add([]byte(`{"action":"evaluate","expression":"1+1"}`))
	f.Add([]byte(`{"action":"wait_visible","selector":"#el"}`))
	f.Add([]byte(`{"action":"tab_list"}`))
	f.Add([]byte(`{"action":"tab_open","url":"https://example.com"}`))
	f.Add([]byte(`{"action":"tab_close","target_id":"t1"}`))
	f.Add([]byte(`{"action":"tab_focus","target_id":"t1"}`))
	f.Add([]byte(`{"action":"status"}`))
	f.Add([]byte(`{"action":"unknown"}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`{"action":"evaluate","expression":"require('fs')"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"action":""}`))

	backend := &mockBrowserBackend{
		contentResult: &PageContent{
			Title: "Test",
			URL:   "https://example.com",
			Text:  "text",
		},
		screenshotData: "base64data",
		evaluateResult: "result",
		tabs: []TabInfo{
			{TargetID: "t1", Title: "Tab 1", URL: "https://example.com", Active: true},
		},
		tabOpenID: "t2",
		statusResult: &BrowserStatus{
			Connected: true,
			Backend:   "mock",
			TabCount:  1,
		},
	}
	bt := NewBrowserTool(backend, slog.Default())

	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := bt.Execute(context.Background(), json.RawMessage(data))
		if err != nil {
			t.Fatalf("Execute should not return error (only IsError in result), got: %v", err)
		}
		if result == nil {
			t.Fatal("result should not be nil")
		}
	})
}
