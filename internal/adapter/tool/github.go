package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// GitHub data types.

// ListReposOpts controls repository listing.
type ListReposOpts struct {
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}

// GitHubRepo describes a GitHub repository.
type GitHubRepo struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	Language    string `json:"language,omitempty"`
	Stars       int    `json:"stars"`
}

// ListIssuesOpts controls issue listing.
type ListIssuesOpts struct {
	State   string `json:"state,omitempty"` // "open", "closed", "all"
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// GitHubIssue describes a GitHub issue.
type GitHubIssue struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	State   string   `json:"state"`
	HTMLURL string   `json:"html_url"`
	Labels  []string `json:"labels,omitempty"`
}

// ListPRsOpts controls pull request listing.
type ListPRsOpts struct {
	State   string `json:"state,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// GitHubPR describes a GitHub pull request.
type GitHubPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Head    string `json:"head"`
	Base    string `json:"base"`
}

// SearchCodeOpts controls code search.
type SearchCodeOpts struct {
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}

// GitHubCodeResult describes a code search hit.
type GitHubCodeResult struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	HTMLURL    string `json:"html_url"`
}

// GitHubBackend abstracts GitHub API operations.
type GitHubBackend interface {
	ListRepos(ctx context.Context, opts ListReposOpts) ([]GitHubRepo, error)
	ListIssues(ctx context.Context, owner, repo string, opts ListIssuesOpts) ([]GitHubIssue, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error)
	CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*GitHubIssue, error)
	ListPRs(ctx context.Context, owner, repo string, opts ListPRsOpts) ([]GitHubPR, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*GitHubPR, error)
	CreatePR(ctx context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error)
	SearchCode(ctx context.Context, query string, opts SearchCodeOpts) ([]GitHubCodeResult, error)
}

// MockGitHubBackend is a no-op backend for testing/development.
type MockGitHubBackend struct{}

func (MockGitHubBackend) ListRepos(_ context.Context, _ ListReposOpts) ([]GitHubRepo, error) {
	return nil, nil
}
func (MockGitHubBackend) ListIssues(_ context.Context, _, _ string, _ ListIssuesOpts) ([]GitHubIssue, error) {
	return nil, nil
}
func (MockGitHubBackend) GetIssue(_ context.Context, _, _ string, _ int) (*GitHubIssue, error) {
	return nil, fmt.Errorf("not found")
}
func (MockGitHubBackend) CreateIssue(_ context.Context, _, _, _, _ string, _ []string) (*GitHubIssue, error) {
	return &GitHubIssue{Number: 1, Title: "mock"}, nil
}
func (MockGitHubBackend) ListPRs(_ context.Context, _, _ string, _ ListPRsOpts) ([]GitHubPR, error) {
	return nil, nil
}
func (MockGitHubBackend) GetPR(_ context.Context, _, _ string, _ int) (*GitHubPR, error) {
	return nil, fmt.Errorf("not found")
}
func (MockGitHubBackend) CreatePR(_ context.Context, _, _, _, _, _, _ string) (*GitHubPR, error) {
	return &GitHubPR{Number: 1, Title: "mock"}, nil
}
func (MockGitHubBackend) SearchCode(_ context.Context, _ string, _ SearchCodeOpts) ([]GitHubCodeResult, error) {
	return nil, nil
}

// GitHubTool provides GitHub operations to the LLM.
type GitHubTool struct {
	backend     GitHubBackend
	logger      *slog.Logger
	rateLimiter *RateLimiter

	// TTL cache for list_repos.
	mu         sync.Mutex
	reposCache []GitHubRepo
	cacheTime  time.Time
	cacheTTL   time.Duration
}

// NewGitHubTool creates a GitHub tool. If backend is nil, a MockGitHubBackend is used.
func NewGitHubTool(backend GitHubBackend, timeout time.Duration, maxReqPerMin int, logger *slog.Logger) *GitHubTool {
	if backend == nil {
		backend = MockGitHubBackend{}
	}
	return &GitHubTool{
		backend:     backend,
		logger:      logger,
		rateLimiter: NewRateLimiter(maxReqPerMin, time.Minute),
		cacheTTL:    5 * time.Minute,
	}
}

func (t *GitHubTool) Name() string { return "github" }
func (t *GitHubTool) Description() string {
	return "Interact with GitHub: list repos, manage issues and pull requests, and search code."
}

func (t *GitHubTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["list_repos", "list_issues", "get_issue", "create_issue", "list_prs", "get_pr", "create_pr", "search_code"],
					"description": "The GitHub action to perform"
				},
				"owner": {
					"type": "string",
					"description": "Repository owner (org or user)"
				},
				"repo": {
					"type": "string",
					"description": "Repository name"
				},
				"number": {
					"type": "integer",
					"description": "Issue or PR number"
				},
				"title": {
					"type": "string",
					"description": "Title for creating issues/PRs"
				},
				"body": {
					"type": "string",
					"description": "Body text for issues/PRs"
				},
				"labels": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Labels for creating issues"
				},
				"head": {
					"type": "string",
					"description": "Head branch for creating PRs"
				},
				"base": {
					"type": "string",
					"description": "Base branch for creating PRs"
				},
				"query": {
					"type": "string",
					"description": "Search query for code search"
				},
				"state": {
					"type": "string",
					"enum": ["open", "closed", "all"],
					"description": "Filter by state (issues/PRs)"
				},
				"page": {
					"type": "integer",
					"description": "Page number for pagination"
				},
				"per_page": {
					"type": "integer",
					"description": "Results per page"
				}
			},
			"required": ["action"]
		}`),
	}
}

type githubParams struct {
	Action  string   `json:"action"`
	Owner   string   `json:"owner,omitempty"`
	Repo    string   `json:"repo,omitempty"`
	Number  int      `json:"number,omitempty"`
	Title   string   `json:"title,omitempty"`
	Body    string   `json:"body,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	Head    string   `json:"head,omitempty"`
	Base    string   `json:"base,omitempty"`
	Query   string   `json:"query,omitempty"`
	State   string   `json:"state,omitempty"`
	Page    int      `json:"page,omitempty"`
	PerPage int      `json:"per_page,omitempty"`
}

func (t *GitHubTool) Execute(ctx context.Context, params json.RawMessage) (*domain.ToolResult, error) {
	return Execute(ctx, "tool.github", t.logger, params,
		Dispatch(func(p githubParams) string { return p.Action }, ActionMap[githubParams]{
			"list_repos":   t.handleListRepos,
			"list_issues":  t.handleListIssues,
			"get_issue":    t.handleGetIssue,
			"create_issue": t.handleCreateIssue,
			"list_prs":     t.handleListPRs,
			"get_pr":       t.handleGetPR,
			"create_pr":    t.handleCreatePR,
			"search_code":  t.handleSearchCode,
		}),
	)
}

func (t *GitHubTool) checkRateLimit() error {
	if !t.rateLimiter.Allow() {
		return domain.ErrRateLimit
	}
	return nil
}

func (t *GitHubTool) requireRepo(p githubParams) error {
	return RequireFields("owner", p.Owner, "repo", p.Repo)
}

func (t *GitHubTool) handleListRepos(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}

	// Check cache.
	t.mu.Lock()
	if t.reposCache != nil && time.Since(t.cacheTime) < t.cacheTTL {
		cached := t.reposCache
		t.mu.Unlock()
		return cached, nil
	}
	t.mu.Unlock()

	repos, err := t.backend.ListRepos(ctx, ListReposOpts{Page: p.Page, PerPage: p.PerPage})
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.reposCache = repos
	t.cacheTime = time.Now()
	t.mu.Unlock()

	if len(repos) == 0 {
		return TextResult("No repositories found."), nil
	}
	return repos, nil
}

func (t *GitHubTool) handleListIssues(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	issues, err := t.backend.ListIssues(ctx, p.Owner, p.Repo, ListIssuesOpts{
		State: p.State, Page: p.Page, PerPage: p.PerPage,
	})
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return TextResult("No issues found."), nil
	}
	return issues, nil
}

func (t *GitHubTool) handleGetIssue(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	if p.Number <= 0 {
		return nil, fmt.Errorf("'number' is required and must be > 0")
	}
	return t.backend.GetIssue(ctx, p.Owner, p.Repo, p.Number)
}

func (t *GitHubTool) handleCreateIssue(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	if err := RequireField("title", p.Title); err != nil {
		return nil, err
	}
	return t.backend.CreateIssue(ctx, p.Owner, p.Repo, p.Title, p.Body, p.Labels)
}

func (t *GitHubTool) handleListPRs(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	prs, err := t.backend.ListPRs(ctx, p.Owner, p.Repo, ListPRsOpts{
		State: p.State, Page: p.Page, PerPage: p.PerPage,
	})
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return TextResult("No pull requests found."), nil
	}
	return prs, nil
}

func (t *GitHubTool) handleGetPR(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	if p.Number <= 0 {
		return nil, fmt.Errorf("'number' is required and must be > 0")
	}
	return t.backend.GetPR(ctx, p.Owner, p.Repo, p.Number)
}

func (t *GitHubTool) handleCreatePR(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := t.requireRepo(p); err != nil {
		return nil, err
	}
	if err := RequireFields("title", p.Title, "head", p.Head, "base", p.Base); err != nil {
		return nil, err
	}
	return t.backend.CreatePR(ctx, p.Owner, p.Repo, p.Title, p.Body, p.Head, p.Base)
}

func (t *GitHubTool) handleSearchCode(ctx context.Context, p githubParams) (any, error) {
	if err := t.checkRateLimit(); err != nil {
		return nil, err
	}
	if err := RequireField("query", p.Query); err != nil {
		return nil, err
	}
	results, err := t.backend.SearchCode(ctx, p.Query, SearchCodeOpts{
		Page: p.Page, PerPage: p.PerPage,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return TextResult("No code results found."), nil
	}
	return results, nil
}
