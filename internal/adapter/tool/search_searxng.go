package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const maxSearchBodySize = 512 * 1024 // 512KB

// searxngResponse models the relevant portion of the SearXNG JSON response.
type searxngResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
		Engine  string `json:"engine"`
	} `json:"results"`
	NumberOfResults int `json:"number_of_results"`
}

// SearXNGBackend searches the web via a SearXNG instance.
type SearXNGBackend struct {
	client      *http.Client
	instanceURL string
	logger      *slog.Logger
}

// NewSearXNGBackend creates a search backend backed by a SearXNG instance.
func NewSearXNGBackend(instanceURL string, logger *slog.Logger) *SearXNGBackend {
	return &SearXNGBackend{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		instanceURL: strings.TrimRight(instanceURL, "/"),
		logger:      logger,
	}
}

func (b *SearXNGBackend) Name() string { return "searxng" }

func (b *SearXNGBackend) Search(ctx context.Context, query string, count int, timeRange string) ([]SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.instanceURL+"/search", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("pageno", "1")
	if timeRange != "" {
		q.Set("time_range", timeRange)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBodySize))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var searxResp searxngResponse
	if err := json.Unmarshal(body, &searxResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(searxResp.Results))
	for _, r := range searxResp.Results {
		if len(results) >= count {
			break
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
		})
	}

	b.logger.Debug("searxng search completed", "query", query, "results", len(results))
	return results, nil
}
