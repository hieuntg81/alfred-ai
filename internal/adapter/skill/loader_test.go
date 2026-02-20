package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alfred-ai/internal/domain"
)

func TestFileSkillProviderLoad(t *testing.T) {
	dir := t.TempDir()

	skillContent := `---
name: code_review
description: Perform a thorough code review
tags: [development, review]
trigger: tool
---
Analyze the following code for bugs, security, performance, and style:
{{.input}}`

	if err := os.WriteFile(filepath.Join(dir, "code-review.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	provider := NewFileSkillProvider(dir)
	skills, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	s := skills[0]
	if s.Name != "code_review" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Description != "Perform a thorough code review" {
		t.Errorf("Description = %q", s.Description)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "development" || s.Tags[1] != "review" {
		t.Errorf("Tags = %v", s.Tags)
	}
	if s.Trigger != "tool" {
		t.Errorf("Trigger = %q", s.Trigger)
	}
}

func TestFileSkillProviderGet(t *testing.T) {
	dir := t.TempDir()

	content := `---
name: test_skill
description: A test skill
trigger: prompt
---
Do something with: {{.input}}`

	os.WriteFile(filepath.Join(dir, "test.md"), []byte(content), 0644)

	provider := NewFileSkillProvider(dir)
	provider.Load(context.Background())

	s, err := provider.Get("test_skill")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Name != "test_skill" {
		t.Errorf("Name = %q", s.Name)
	}

	_, err = provider.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestFileSkillProviderList(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"a", "b", "c"} {
		content := "---\nname: " + name + "\ndescription: skill " + name + "\n---\ntemplate"
		os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644)
	}

	provider := NewFileSkillProvider(dir)
	provider.Load(context.Background())

	list := provider.List()
	if len(list) != 3 {
		t.Errorf("expected 3 skills, got %d", len(list))
	}
}

func TestFileSkillProviderMissingDir(t *testing.T) {
	provider := NewFileSkillProvider("/nonexistent/dir")
	_, err := provider.Load(context.Background())
	if err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestFileSkillProviderNonMdFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a skill"), 0644)
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: real\ndescription: d\n---\nbody"), 0644)

	provider := NewFileSkillProvider(dir)
	skills, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 {
		t.Errorf("expected 1 skill (skip .txt), got %d", len(skills))
	}
}

func TestSkillToolExecute(t *testing.T) {
	dir := t.TempDir()

	content := `---
name: summarize
description: Summarize text
trigger: tool
---
Please summarize the following:
{{.input}}`

	os.WriteFile(filepath.Join(dir, "summarize.md"), []byte(content), 0644)

	provider := NewFileSkillProvider(dir)
	skills, _ := provider.Load(context.Background())

	tool := NewSkillTool(skills[0])

	if tool.Name() != "summarize" {
		t.Errorf("Name = %q", tool.Name())
	}

	schema := tool.Schema()
	if schema.Name != "summarize" {
		t.Errorf("Schema name = %q", schema.Name)
	}

	params, _ := json.Marshal(map[string]string{"input": "some code here"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Please summarize the following:\nsome code here" {
		t.Errorf("result = %q", result.Content)
	}
}

func TestSkillToolInvalidParams(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: test
description: test
trigger: tool
---
{{.input}}`
	os.WriteFile(filepath.Join(dir, "test.md"), []byte(content), 0644)

	provider := NewFileSkillProvider(dir)
	skills, _ := provider.Load(context.Background())
	tool := NewSkillTool(skills[0])

	result, err := tool.Execute(context.Background(), json.RawMessage(`invalid json`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestParseSkillFileMissingFrontmatter(t *testing.T) {
	_, err := parseSkillFile("no frontmatter here")
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseSkillFileMissingName(t *testing.T) {
	_, err := parseSkillFile("---\ndescription: test\n---\nbody")
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestSkillToolDescription(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: test_desc
description: A detailed description
trigger: tool
---
{{.input}}`
	os.WriteFile(filepath.Join(dir, "test.md"), []byte(content), 0644)

	provider := NewFileSkillProvider(dir)
	skills, _ := provider.Load(context.Background())
	tool := NewSkillTool(skills[0])

	if got := tool.Description(); got != "A detailed description" {
		t.Errorf("Description() = %q, want %q", got, "A detailed description")
	}
}

func TestParseSkillFileDefaultTrigger(t *testing.T) {
	s, err := parseSkillFile("---\nname: test\ndescription: d\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if s.Trigger != "tool" {
		t.Errorf("default trigger = %q, want tool", s.Trigger)
	}
}

// --- Additional tests for coverage ---

// TestFileSkillProviderLoadReadFileError covers the ReadFile error path in Load.
// We create a .md file that can't be read.
func TestFileSkillProviderLoadReadFileError(t *testing.T) {
	dir := t.TempDir()

	// Create a .md file with no read permissions
	path := filepath.Join(dir, "unreadable.md")
	os.WriteFile(path, []byte("---\nname: test\n---\nbody"), 0644)
	os.Chmod(path, 0000)
	defer os.Chmod(path, 0644)

	provider := NewFileSkillProvider(dir)
	_, err := provider.Load(context.Background())
	if err == nil {
		t.Error("expected error for unreadable file")
	}
}

// TestFileSkillProviderLoadParseError covers the parseSkillFile error path in Load.
func TestFileSkillProviderLoadParseError(t *testing.T) {
	dir := t.TempDir()

	// Create a .md file with invalid skill format (no frontmatter)
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no frontmatter here"), 0644)

	provider := NewFileSkillProvider(dir)
	_, err := provider.Load(context.Background())
	if err == nil {
		t.Error("expected error for invalid skill file")
	}
}

// TestFileSkillProviderLoadSubdirectoryWithSKILLMD covers the subdirectory layout.
func TestFileSkillProviderLoadSubdirectoryWithSKILLMD(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory with SKILL.md
	os.MkdirAll(filepath.Join(dir, "summarize"), 0755)
	os.WriteFile(filepath.Join(dir, "summarize", "SKILL.md"),
		[]byte("---\nname: summarize\ndescription: Summarize text\ntrigger: prompt\n---\n{{.input}}"), 0644)

	// Create a flat .md skill too
	os.WriteFile(filepath.Join(dir, "flat.md"),
		[]byte("---\nname: flat\ndescription: d\n---\nbody"), 0644)

	provider := NewFileSkillProvider(dir)
	skills, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 skills (subdir + flat), got %d", len(skills))
	}
}

// TestFileSkillProviderLoadSkipsSubdirWithoutSKILLMD covers subdirs without SKILL.md.
func TestFileSkillProviderLoadSkipsSubdirWithoutSKILLMD(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory without SKILL.md
	os.MkdirAll(filepath.Join(dir, "empty-subdir"), 0755)

	// Create a valid flat skill file
	os.WriteFile(filepath.Join(dir, "valid.md"),
		[]byte("---\nname: valid\ndescription: d\n---\nbody"), 0644)

	provider := NewFileSkillProvider(dir)
	skills, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 {
		t.Errorf("expected 1 skill (skip empty subdir), got %d", len(skills))
	}
}

// TestParseSkillFileMissingClosingDelimiter covers the missing closing --- delimiter.
func TestParseSkillFileMissingClosingDelimiter(t *testing.T) {
	_, err := parseSkillFile("---\nname: test\ndescription: d")
	if err == nil {
		t.Error("expected error for missing closing frontmatter delimiter")
	}
}

// TestParseSkillFileWithMetadata covers the default metadata key parsing branch.
func TestParseSkillFileWithMetadata(t *testing.T) {
	content := `---
name: meta_skill
description: A skill with custom metadata
trigger: prompt
author: test-author
version: 1.0
---
template body`

	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if s.Name != "meta_skill" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Metadata["author"] != "test-author" {
		t.Errorf("Metadata[author] = %q", s.Metadata["author"])
	}
	if s.Metadata["version"] != "1.0" {
		t.Errorf("Metadata[version] = %q", s.Metadata["version"])
	}
}

// TestParseSkillFileWithColonlessLine covers the colonIdx < 0 continue branch.
func TestParseSkillFileWithColonlessLine(t *testing.T) {
	content := "---\nname: coltest\ndescription: d\nno-colon-here\n---\nbody"
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if s.Name != "coltest" {
		t.Errorf("Name = %q, want %q", s.Name, "coltest")
	}
}

// TestParseSkillFileWithEmptyLine covers the empty line continue branch in frontmatter parsing.
func TestParseSkillFileWithEmptyLine(t *testing.T) {
	content := "---\nname: emptyline\n\ndescription: d\n---\nbody"
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if s.Name != "emptyline" {
		t.Errorf("Name = %q, want %q", s.Name, "emptyline")
	}
}

// TestParseSkillFileWithTags covers the tags parsing branch.
func TestParseSkillFileWithTagsNoBrackets(t *testing.T) {
	content := "---\nname: tagtest\ndescription: d\ntags: code, review, test\n---\nbody"
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if len(s.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(s.Tags), s.Tags)
	}
}

// TestParseSkillFileWithTools covers the tools field parsing.
func TestParseSkillFileWithTools(t *testing.T) {
	content := `---
name: extract
description: Extract structured data
trigger: tool
tools: [web_search, browser]
---
{{.input}}`
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if len(s.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(s.Tools), s.Tools)
	}
	if s.Tools[0] != "web_search" || s.Tools[1] != "browser" {
		t.Errorf("Tools = %v", s.Tools)
	}
}

// TestParseSkillFileWithModelPreference covers the model_preference field parsing.
func TestParseSkillFileWithModelPreference(t *testing.T) {
	content := `---
name: summarize
description: Summarize text
trigger: prompt
model_preference: fast
---
{{.input}}`
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if s.ModelPreference != "fast" {
		t.Errorf("ModelPreference = %q, want %q", s.ModelPreference, "fast")
	}
}

// TestParseSkillFileWithAllNewFields covers both tools and model_preference together.
func TestParseSkillFileWithAllNewFields(t *testing.T) {
	content := `---
name: analyze
description: Analyze data
trigger: tool
tools: [browser]
model_preference: powerful
tags: [analysis, data]
---
Analyze: {{.input}}`
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if s.Name != "analyze" {
		t.Errorf("Name = %q", s.Name)
	}
	if len(s.Tools) != 1 || s.Tools[0] != "browser" {
		t.Errorf("Tools = %v", s.Tools)
	}
	if s.ModelPreference != "powerful" {
		t.Errorf("ModelPreference = %q", s.ModelPreference)
	}
	if len(s.Tags) != 2 {
		t.Errorf("Tags = %v", s.Tags)
	}
}

// TestParseSkillFileEmptyTools covers empty tools field.
func TestParseSkillFileEmptyTools(t *testing.T) {
	content := `---
name: simple
description: Simple skill
trigger: prompt
tools: []
---
{{.input}}`
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if len(s.Tools) != 0 {
		t.Errorf("expected empty tools, got %v", s.Tools)
	}
}

// TestLoadDuplicateSkillName ensures duplicate skill names return an error.
func TestLoadDuplicateSkillName(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"first.md", "second.md"} {
		content := "---\nname: duplicate_name\ndescription: d\ntrigger: tool\n---\nbody"
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}

	provider := NewFileSkillProvider(dir)
	_, err := provider.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for duplicate skill name")
	}
	if !strings.Contains(err.Error(), "duplicate skill name") {
		t.Errorf("error = %q, want to contain 'duplicate skill name'", err.Error())
	}
}

// TestParseSkillFileInvalidTrigger ensures invalid trigger values are rejected.
func TestParseSkillFileInvalidTrigger(t *testing.T) {
	_, err := parseSkillFile("---\nname: test\ndescription: d\ntrigger: invalid\n---\nbody")
	if err == nil {
		t.Fatal("expected error for invalid trigger")
	}
	if !strings.Contains(err.Error(), "invalid trigger") {
		t.Errorf("error = %q, want to contain 'invalid trigger'", err.Error())
	}
}

// TestLoadLargeFile ensures files exceeding maxSkillFileSize are rejected.
func TestLoadLargeFile(t *testing.T) {
	dir := t.TempDir()

	// Create a file larger than 1 MiB
	largeContent := make([]byte, maxSkillFileSize+1)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	os.WriteFile(filepath.Join(dir, "huge.md"), largeContent, 0644)

	provider := NewFileSkillProvider(dir)
	_, err := provider.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for oversized skill file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want to contain 'too large'", err.Error())
	}
}

// TestSkillToolExecuteEmptyInput ensures empty input logs warning but succeeds.
func TestSkillToolExecuteEmptyInput(t *testing.T) {
	s := domain.Skill{
		Name:        "test_empty",
		Description: "test",
		Trigger:     "tool",
		Template:    "Process: {{.input}}",
	}
	tool := NewSkillTool(s)

	params, _ := json.Marshal(map[string]string{"input": ""})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Process: " {
		t.Errorf("result = %q, want 'Process: '", result.Content)
	}
}

// TestSkillToolExecuteWithModelRouter tests model routing path.
func TestSkillToolExecuteWithModelRouter(t *testing.T) {
	s := domain.Skill{
		Name:            "routed",
		Description:     "test",
		Trigger:         "tool",
		Template:        "Summarize: {{.input}}",
		ModelPreference: "fast",
	}

	mockRouter := &mockModelRouter{
		provider: &mockLLMProvider{
			response: "Summarized result",
		},
	}

	tool := NewSkillTool(s, WithModelRouter(mockRouter))

	params, _ := json.Marshal(map[string]string{"input": "some text"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Summarized result" {
		t.Errorf("result = %q, want 'Summarized result'", result.Content)
	}
	if mockRouter.lastPreference != "fast" {
		t.Errorf("router called with preference %q, want 'fast'", mockRouter.lastPreference)
	}
}

// TestSkillToolExecuteRouterError tests fallback when routing fails.
func TestSkillToolExecuteRouterError(t *testing.T) {
	s := domain.Skill{
		Name:            "routed_err",
		Description:     "test",
		Trigger:         "tool",
		Template:        "Do: {{.input}}",
		ModelPreference: "unknown",
	}

	mockRouter := &mockModelRouter{
		err: fmt.Errorf("no provider for preference"),
	}

	tool := NewSkillTool(s, WithModelRouter(mockRouter))

	params, _ := json.Marshal(map[string]string{"input": "data"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should fall back to rendered template
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Do: data" {
		t.Errorf("result = %q, want 'Do: data'", result.Content)
	}
}

// TestSkillToolExecuteLLMCallError tests error from LLM provider.
func TestSkillToolExecuteLLMCallError(t *testing.T) {
	s := domain.Skill{
		Name:            "llm_err",
		Description:     "test",
		Trigger:         "tool",
		Template:        "Do: {{.input}}",
		ModelPreference: "fast",
	}

	mockRouter := &mockModelRouter{
		provider: &mockLLMProvider{
			err: fmt.Errorf("api rate limited"),
		},
	}

	tool := NewSkillTool(s, WithModelRouter(mockRouter))

	params, _ := json.Marshal(map[string]string{"input": "data"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for LLM call failure")
	}
	if !result.IsRetryable {
		t.Error("expected IsRetryable for LLM call failure")
	}
}

// TestSkillToolExecuteNoRouterNoPreference tests that without router, template is returned.
func TestSkillToolExecuteNoRouterNoPreference(t *testing.T) {
	s := domain.Skill{
		Name:        "no_route",
		Description: "test",
		Trigger:     "tool",
		Template:    "Plain: {{.input}}",
	}

	tool := NewSkillTool(s)

	params, _ := json.Marshal(map[string]string{"input": "hi"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "Plain: hi" {
		t.Errorf("result = %q, want 'Plain: hi'", result.Content)
	}
}

// --- Mock types for model routing tests ---

type mockModelRouter struct {
	provider       domain.LLMProvider
	err            error
	lastPreference string
}

func (m *mockModelRouter) Route(preference string) (domain.LLMProvider, error) {
	m.lastPreference = preference
	return m.provider, m.err
}

type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) Chat(_ context.Context, _ domain.ChatRequest) (*domain.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domain.ChatResponse{
		Message: domain.Message{
			Role:    domain.RoleAssistant,
			Content: m.response,
		},
	}, nil
}

func (m *mockLLMProvider) Name() string { return "mock" }

// TestParseSkillFileValidTriggers ensures all valid trigger values are accepted.
func TestParseSkillFileValidTriggers(t *testing.T) {
	for _, trigger := range []string{"tool", "prompt", "both"} {
		content := fmt.Sprintf("---\nname: test_%s\ndescription: d\ntrigger: %s\n---\nbody", trigger, trigger)
		s, err := parseSkillFile(content)
		if err != nil {
			t.Errorf("trigger %q: unexpected error: %v", trigger, err)
		}
		if s.Trigger != trigger {
			t.Errorf("trigger = %q, want %q", s.Trigger, trigger)
		}
	}
}

// TestLoadAllSkillsFromProjectDir loads all skills from the project's skills directory.
func TestLoadAllSkillsFromProjectDir(t *testing.T) {
	// This test verifies all skill files in the project parse correctly.
	dir := "../../../skills"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("skills directory not found (not running from project root)")
	}

	provider := NewFileSkillProvider(dir)
	skills, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("Load from project skills dir: %v", err)
	}

	if len(skills) < 35 {
		t.Errorf("expected at least 35 skills, got %d", len(skills))
	}

	// Verify no duplicate names (already enforced by loader, but double-check).
	seen := make(map[string]bool)
	for _, s := range skills {
		if seen[s.Name] {
			t.Errorf("duplicate skill name: %s", s.Name)
		}
		seen[s.Name] = true

		// Verify required fields.
		if s.Name == "" {
			t.Error("skill with empty name")
		}
		if s.Description == "" {
			t.Errorf("skill %s has empty description", s.Name)
		}
		if s.Template == "" {
			t.Errorf("skill %s has empty template", s.Name)
		}
		if s.Trigger != "tool" && s.Trigger != "prompt" && s.Trigger != "both" {
			t.Errorf("skill %s has invalid trigger: %q", s.Name, s.Trigger)
		}
	}
}

// TestParseSkillFileMissingNewFields covers backward compatibility — missing new fields default to empty.
func TestParseSkillFileMissingNewFields(t *testing.T) {
	content := `---
name: legacy
description: A legacy skill without new fields
trigger: prompt
---
{{.input}}`
	s, err := parseSkillFile(content)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if len(s.Tools) != 0 {
		t.Errorf("expected nil/empty tools, got %v", s.Tools)
	}
	if s.ModelPreference != "" {
		t.Errorf("expected empty model_preference, got %q", s.ModelPreference)
	}
}
