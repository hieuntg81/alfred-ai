# Skills Index

alfred-ai ships with 35 built-in skills. Skills are reusable prompt/tool templates that extend the agent's capabilities without custom code.

## Enabling Skills

```yaml
skills:
  enabled: true
  dir: ./skills
```

Skills are loaded from YAML files in the skills directory. Each skill defines a trigger type:

- **prompt** — Injected into the system prompt context automatically
- **tool** — Exposed as a callable tool for the LLM

## Text & Writing

| Skill | Description | Trigger |
|-------|-------------|---------|
| `summarize` | Summarize long text into concise key points | prompt |
| `translate` | Translate text between languages while preserving tone and meaning | prompt |
| `rewrite` | Rewrite text to improve clarity, tone, or style | prompt |
| `proofread` | Proofread text for grammar, spelling, and punctuation errors | prompt |
| `draft-email` | Draft a professional email from key points or a brief description | prompt |
| `meeting-notes` | Transform raw meeting notes or transcripts into structured summaries | prompt |
| `reply-suggest` | Suggest reply options for a received message | prompt |
| `generate-report` | Generate a structured report from raw data or notes | prompt |
| `changelog` | Generate a changelog from git history or commit messages | tool |

## Development & Code

| Skill | Description | Trigger |
|-------|-------------|---------|
| `code-review` | Perform a thorough code review for bugs, security, and best practices | prompt |
| `explain-code` | Explain code in plain language, including logic flow and design decisions | prompt |
| `write-tests` | Generate comprehensive test cases for the given code | prompt |
| `debug` | Analyze code or error output to identify and fix bugs | prompt |
| `git-commit` | Generate a conventional commit message from a diff or description | prompt |
| `refactor` | Suggest refactoring improvements with concrete before/after examples | prompt |
| `api-design` | Design REST or GraphQL APIs with endpoints, schemas, and best practices | prompt |
| `sql-query` | Write, optimize, and explain SQL queries | prompt |
| `regex-builder` | Build and explain regular expressions with test cases | prompt |
| `dockerfile` | Create or optimize Dockerfiles with multi-stage builds and best practices | prompt |
| `security-review` | Review code for security vulnerabilities and suggest fixes | prompt |
| `json-schema` | Generate JSON Schema from examples or descriptions | prompt |

## Research & Analysis

| Skill | Description | Trigger | Tools Used |
|-------|-------------|---------|------------|
| `web-research` | Research a topic using web search and compile findings | prompt | web_search |
| `web-lookup` | Search the web and summarize results for a given query | tool | web_search |
| `site-reader` | Read a website and extract structured information | tool | web_search, browser |
| `fact-check` | Verify claims and statements for accuracy | prompt | web_search |
| `extract-data` | Extract structured data (dates, names, amounts) from unstructured text | tool | — |
| `compare` | Compare items, options, or approaches with structured analysis | tool | — |
| `analyze-data` | Analyze structured or semi-structured data and provide insights | tool | — |

## Productivity & Planning

| Skill | Description | Trigger |
|-------|-------------|---------|
| `schedule` | Help organize and plan schedules, agendas, or time blocks | prompt |
| `daily-brief` | Generate a daily briefing from tasks, calendar, and messages | prompt |
| `brainstorm` | Structured brainstorming with divergent and convergent thinking phases | prompt |
| `decision-matrix` | Create a weighted decision matrix to compare options objectively | prompt |
| `project-plan` | Break down a project into tasks with dependencies and effort estimates | prompt |
| `presentation` | Create a structured presentation outline with speaker notes | prompt |
| `swot-analysis` | Perform a SWOT analysis (Strengths, Weaknesses, Opportunities, Threats) | prompt |

## Creating Custom Skills

Skills are defined as YAML files in the skills directory. Each skill lives in its own subdirectory:

```
skills/
  my-skill/
    SKILL.md       # Skill definition
```

See the built-in skills in `skills/` for examples of the YAML format.
