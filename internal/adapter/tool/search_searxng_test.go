package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSearXNGBackendName(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	if b.Name() != "searxng" {
		t.Errorf("Name() = %q, want %q", b.Name(), "searxng")
	}
}

func TestSearXNGBackendTrailingSlashTrimmed(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080/", newTestLogger())
	if b.instanceURL != "http://localhost:8080" {
		t.Errorf("instanceURL = %q, want trailing slash trimmed", b.instanceURL)
	}
}

func TestSearXNGBackendSuccess(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Accept"); got != "application/json" {
				t.Errorf("Accept header = %q, want %q", got, "application/json")
			}
			if got := req.URL.Query().Get("q"); got != "golang testing" {
				t.Errorf("query param = %q, want %q", got, "golang testing")
			}
			if got := req.URL.Query().Get("format"); got != "json" {
				t.Errorf("format param = %q, want %q", got, "json")
			}

			body := `{"results":[{"title":"Go Testing","url":"https://go.dev/testing","content":"Testing in Go"}]}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	results, err := b.Search(context.Background(), "golang testing", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Go Testing" {
		t.Errorf("title = %q, want %q", results[0].Title, "Go Testing")
	}
	if results[0].URL != "https://go.dev/testing" {
		t.Errorf("url = %q, want %q", results[0].URL, "https://go.dev/testing")
	}
}

func TestSearXNGBackendHTTPError(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		}),
	}

	_, err := b.Search(context.Background(), "test", 5, "")
	if err == nil {
		t.Error("expected error for HTTP failure")
	}
}

func TestSearXNGBackendNon200Status(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 429,
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := b.Search(context.Background(), "test", 5, "")
	if err == nil {
		t.Error("expected error for 429 status")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected status code in error, got: %v", err)
	}
}

func TestSearXNGBackendBodyReadError(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(&errReader{}),
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := b.Search(context.Background(), "test", 5, "")
	if err == nil {
		t.Error("expected error for body read failure")
	}
}

func TestSearXNGBackendInvalidResponseJSON(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := b.Search(context.Background(), "test", 5, "")
	if err == nil {
		t.Error("expected error for invalid response JSON")
	}
}

func TestSearXNGBackendTimeRangeParam(t *testing.T) {
	var receivedTimeRange string
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())
	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedTimeRange = req.URL.Query().Get("time_range")
			body := `{"results":[]}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	b.Search(context.Background(), "test", 5, "day")
	if receivedTimeRange != "day" {
		t.Errorf("time_range = %q, want %q", receivedTimeRange, "day")
	}
}

func TestSearXNGBackendCountLimit(t *testing.T) {
	b := NewSearXNGBackend("http://localhost:8080", newTestLogger())

	var results []string
	for i := 0; i < 10; i++ {
		results = append(results, fmt.Sprintf(`{"title":"R%d","url":"https://example.com/%d","content":"d%d"}`, i, i, i))
	}
	responseBody := `{"results":[` + strings.Join(results, ",") + `]}`

	b.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	got, err := b.Search(context.Background(), "test", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 results, got %d", len(got))
	}
}
