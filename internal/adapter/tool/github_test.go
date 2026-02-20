package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// --- test backend ---

type testGitHubBackend struct {
	repos   []GitHubRepo
	issues  map[string][]GitHubIssue // key: "owner/repo"
	prs     map[string][]GitHubPR
	code    []GitHubCodeResult
	nextNum int

	listReposErr   error
	listIssuesErr  error
	getIssueErr    error
	createIssueErr error
	listPRsErr     error
	getPRErr       error
	createPRErr    error
	searchCodeErr  error
}

func newTestGitHubBackend() *testGitHubBackend {
	return &testGitHubBackend{
		issues:  make(map[string][]GitHubIssue),
		prs:     make(map[string][]GitHubPR),
		nextNum: 1,
	}
}

func (b *testGitHubBackend) ListRepos(_ context.Context, _ ListReposOpts) ([]GitHubRepo, error) {
	if b.listReposErr != nil {
		return nil, b.listReposErr
	}
	return b.repos, nil
}

func (b *testGitHubBackend) ListIssues(_ context.Context, owner, repo string, _ ListIssuesOpts) ([]GitHubIssue, error) {
	if b.listIssuesErr != nil {
		return nil, b.listIssuesErr
	}
	return b.issues[owner+"/"+repo], nil
}

func (b *testGitHubBackend) GetIssue(_ context.Context, owner, repo string, number int) (*GitHubIssue, error) {
	if b.getIssueErr != nil {
		return nil, b.getIssueErr
	}
	for _, iss := range b.issues[owner+"/"+repo] {
		if iss.Number == number {
			return &iss, nil
		}
	}
	return nil, fmt.Errorf("issue #%d not found", number)
}

func (b *testGitHubBackend) CreateIssue(_ context.Context, owner, repo, title, body string, labels []string) (*GitHubIssue, error) {
	if b.createIssueErr != nil {
		return nil, b.createIssueErr
	}
	iss := GitHubIssue{Number: b.nextNum, Title: title, Body: body, State: "open", Labels: labels}
	b.nextNum++
	key := owner + "/" + repo
	b.issues[key] = append(b.issues[key], iss)
	return &iss, nil
}

func (b *testGitHubBackend) ListPRs(_ context.Context, owner, repo string, _ ListPRsOpts) ([]GitHubPR, error) {
	if b.listPRsErr != nil {
		return nil, b.listPRsErr
	}
	return b.prs[owner+"/"+repo], nil
}

func (b *testGitHubBackend) GetPR(_ context.Context, owner, repo string, number int) (*GitHubPR, error) {
	if b.getPRErr != nil {
		return nil, b.getPRErr
	}
	for _, pr := range b.prs[owner+"/"+repo] {
		if pr.Number == number {
			return &pr, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", number)
}

func (b *testGitHubBackend) CreatePR(_ context.Context, owner, repo, title, body, head, base string) (*GitHubPR, error) {
	if b.createPRErr != nil {
		return nil, b.createPRErr
	}
	pr := GitHubPR{Number: b.nextNum, Title: title, Body: body, State: "open", Head: head, Base: base}
	b.nextNum++
	key := owner + "/" + repo
	b.prs[key] = append(b.prs[key], pr)
	return &pr, nil
}

func (b *testGitHubBackend) SearchCode(_ context.Context, _ string, _ SearchCodeOpts) ([]GitHubCodeResult, error) {
	if b.searchCodeErr != nil {
		return nil, b.searchCodeErr
	}
	return b.code, nil
}

// --- helpers ---

func newTestGitHubTool(t *testing.T) (*GitHubTool, *testGitHubBackend) {
	t.Helper()
	b := newTestGitHubBackend()
	tool := NewGitHubTool(b, 15*time.Second, 1000, newTestLogger())
	return tool, b
}

func execGitHubTool(t *testing.T, tool *GitHubTool, params any) *domain.ToolResult {
	t.Helper()
	data, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

// --- metadata ---

func TestGitHubToolName(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	if tool.Name() != "github" {
		t.Errorf("got %q, want %q", tool.Name(), "github")
	}
}

func TestGitHubToolDescription(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestGitHubToolSchema(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	schema := tool.Schema()
	if schema.Name != "github" {
		t.Errorf("schema name: got %q, want %q", schema.Name, "github")
	}
	var params map[string]any
	if err := json.Unmarshal(schema.Parameters, &params); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
}

// --- action success tests ---

func TestGitHubToolListRepos(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.repos = []GitHubRepo{{FullName: "org/repo", Stars: 42}}
	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "org/repo") {
		t.Errorf("expected repo in output: %s", result.Content)
	}
}

func TestGitHubToolListReposEmpty(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No repositories") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestGitHubToolListReposCache(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.repos = []GitHubRepo{{FullName: "org/repo"}}

	// First call populates cache.
	execGitHubTool(t, tool, map[string]any{"action": "list_repos"})

	// Change backend data — should still return cached.
	backend.repos = []GitHubRepo{{FullName: "org/other"}}
	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if strings.Contains(result.Content, "org/other") {
		t.Error("expected cached result, got fresh data")
	}
}

func TestGitHubToolListIssues(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.issues["org/repo"] = []GitHubIssue{{Number: 1, Title: "bug"}}
	result := execGitHubTool(t, tool, map[string]any{
		"action": "list_issues", "owner": "org", "repo": "repo",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "bug") {
		t.Errorf("expected issue in output: %s", result.Content)
	}
}

func TestGitHubToolListIssuesEmpty(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "list_issues", "owner": "org", "repo": "repo",
	})
	if !strings.Contains(result.Content, "No issues") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

func TestGitHubToolGetIssue(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.issues["org/repo"] = []GitHubIssue{{Number: 42, Title: "found"}}
	result := execGitHubTool(t, tool, map[string]any{
		"action": "get_issue", "owner": "org", "repo": "repo", "number": 42,
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "found") {
		t.Errorf("expected issue data: %s", result.Content)
	}
}

func TestGitHubToolCreateIssue(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "create_issue", "owner": "org", "repo": "repo",
		"title": "new bug", "body": "details",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "new bug") {
		t.Errorf("expected created issue: %s", result.Content)
	}
}

func TestGitHubToolListPRs(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.prs["org/repo"] = []GitHubPR{{Number: 1, Title: "feat"}}
	result := execGitHubTool(t, tool, map[string]any{
		"action": "list_prs", "owner": "org", "repo": "repo",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "feat") {
		t.Errorf("expected PR in output: %s", result.Content)
	}
}

func TestGitHubToolGetPR(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.prs["org/repo"] = []GitHubPR{{Number: 10, Title: "my-pr"}}
	result := execGitHubTool(t, tool, map[string]any{
		"action": "get_pr", "owner": "org", "repo": "repo", "number": 10,
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestGitHubToolCreatePR(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "create_pr", "owner": "org", "repo": "repo",
		"title": "feat", "head": "feature", "base": "main",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestGitHubToolSearchCode(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.code = []GitHubCodeResult{{Repository: "org/repo", Path: "main.go"}}
	result := execGitHubTool(t, tool, map[string]any{
		"action": "search_code", "query": "func main",
	})
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Errorf("expected code result: %s", result.Content)
	}
}

func TestGitHubToolSearchCodeEmpty(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "search_code", "query": "nonexistent",
	})
	if !strings.Contains(result.Content, "No code results") {
		t.Errorf("expected empty message: %s", result.Content)
	}
}

// --- validation error tests ---

func TestGitHubToolUnknownAction(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{"action": "bad"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestGitHubToolListIssuesMissingRepo(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{"action": "list_issues", "owner": "org"})
	if !result.IsError {
		t.Error("expected error for missing repo")
	}
}

func TestGitHubToolGetIssueMissingNumber(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "get_issue", "owner": "org", "repo": "repo",
	})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
}

func TestGitHubToolCreateIssueMissingTitle(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "create_issue", "owner": "org", "repo": "repo",
	})
	if !result.IsError {
		t.Error("expected error for missing title")
	}
}

func TestGitHubToolCreatePRMissingFields(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "create_pr", "owner": "org", "repo": "repo", "title": "x",
	})
	if !result.IsError {
		t.Error("expected error for missing head/base")
	}
}

func TestGitHubToolSearchCodeMissingQuery(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{"action": "search_code"})
	if !result.IsError {
		t.Error("expected error for missing query")
	}
}

func TestGitHubToolGetPRMissingNumber(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{
		"action": "get_pr", "owner": "org", "repo": "repo",
	})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
}

func TestGitHubToolListPRsMissingRepo(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result := execGitHubTool(t, tool, map[string]any{"action": "list_prs"})
	if !result.IsError {
		t.Error("expected error for missing owner/repo")
	}
}

func TestGitHubToolInvalidJSON(t *testing.T) {
	tool, _ := newTestGitHubTool(t)
	result, err := tool.Execute(context.Background(), []byte(`{invalid`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

// --- rate limiting ---

func TestGitHubToolRateLimit(t *testing.T) {
	b := newTestGitHubBackend()
	tool := NewGitHubTool(b, 15*time.Second, 2, newTestLogger())

	execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	execGitHubTool(t, tool, map[string]any{"action": "list_repos"})

	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if !result.IsError {
		t.Error("expected rate limit error")
	}
}

// --- backend error propagation ---

func TestGitHubToolBackendListReposError(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.listReposErr = fmt.Errorf("api error")
	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestGitHubToolBackendGetIssueError(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.getIssueErr = fmt.Errorf("api error")
	result := execGitHubTool(t, tool, map[string]any{
		"action": "get_issue", "owner": "o", "repo": "r", "number": 1,
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestGitHubToolBackendCreateIssueError(t *testing.T) {
	tool, backend := newTestGitHubTool(t)
	backend.createIssueErr = fmt.Errorf("api error")
	result := execGitHubTool(t, tool, map[string]any{
		"action": "create_issue", "owner": "o", "repo": "r", "title": "t",
	})
	if !result.IsError {
		t.Error("expected error from backend")
	}
}

func TestGitHubToolNilBackendUsesMock(t *testing.T) {
	tool := NewGitHubTool(nil, 15*time.Second, 100, newTestLogger())
	result := execGitHubTool(t, tool, map[string]any{"action": "list_repos"})
	if result.IsError {
		t.Fatalf("expected success with mock: %s", result.Content)
	}
}

// --- fuzz ---

func FuzzGitHubTool_Execute(f *testing.F) {
	f.Add([]byte(`{"action":"list_repos"}`))
	f.Add([]byte(`{"action":"get_issue","owner":"o","repo":"r","number":1}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))

	b := newTestGitHubBackend()
	tool := NewGitHubTool(b, 15*time.Second, 10000, newTestLogger())
	f.Fuzz(func(t *testing.T, data []byte) {
		tool.Execute(context.Background(), data)
	})
}
