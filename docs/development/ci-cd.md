# CI/CD Documentation

This document describes the continuous integration and deployment pipelines for alfred-ai.

## Overview

alfred-ai uses GitHub Actions for automated testing, security scanning, benchmarking, and releases.

## Workflows

### 1. CI Workflow (`.github/workflows/ci.yml`)

**Triggers:**
- Push to `main`, `master`, or `develop` branches
- Pull requests to `main`, `master`, or `develop` branches

**Jobs:**

#### Test
- Runs on Go versions: 1.21, 1.22, 1.23
- Executes all unit tests with `-short` flag
- Generates coverage report (Go 1.23 only)
- Uploads coverage to Codecov

#### Race Detection
- Runs tests with `-race` flag to detect data races
- Uses `CGO_ENABLED=1` (required for race detector)

#### Build
- Cross-compiles for multiple platforms:
  - Linux: amd64, arm64
  - macOS: amd64, arm64
  - Windows: amd64
- Uploads build artifacts (retained for 7 days)

#### Lint
- Runs `golangci-lint` with comprehensive linter set
- Configuration in `.golangci.yml`

#### Vet & Fmt
- `go vet` for code quality issues
- `gofmt` check for formatting

**Status:** Required checks for merging pull requests

---

### 2. Benchmark Workflow (`.github/workflows/benchmark.yml`)

**Triggers:**
- Push to `main` or `master`
- Pull requests to `main` or `master`
- Manual dispatch with optional baseline comparison

**Jobs:**

#### Benchmark
- Runs all benchmarks with `-benchmem` flag
- Runs for 5 seconds per benchmark
- Compares with baseline (if available)
- Posts comparison as PR comment
- Saves results as baseline on main branch

#### Memory Profiling
- Generates memory and CPU profiles
- Analyzes memory usage with `pprof`
- Checks for potential memory leaks
- Uploads profiles as artifacts

**Usage:**
```bash
# Manually trigger benchmark comparison
gh workflow run benchmark.yml -f compare=true
```

---

### 3. Security Workflow (`.github/workflows/security.yml`)

**Triggers:**
- Push to `main` or `master`
- Pull requests
- Weekly schedule (Mondays at 00:00 UTC)
- Manual dispatch

**Jobs:**

#### GoSec
- Static security analysis
- Uploads results as SARIF for GitHub Security
- Checks for common security issues

#### GovulnCheck
- Checks for known vulnerabilities in dependencies
- Uses official Go vulnerability database

#### Dependency Review
- Reviews dependency changes in PRs
- Fails on moderate or higher severity issues

#### Nancy
- Additional dependency vulnerability scanning

#### Secrets Scan
- Uses TruffleHog to detect leaked secrets
- Scans commit history

#### CodeQL
- Deep semantic code analysis
- Runs security and quality queries

**Status:** Informational (doesn't block merges)

---

### 4. Integration Tests (`.github/workflows/integration.yml`)

**Triggers:**
- Nightly schedule (02:00 UTC)
- Manual dispatch with provider selection

**Jobs:**
- Tests OpenAI integration (if `OPENAI_API_KEY` configured)
- Tests Anthropic integration (if `ANTHROPIC_API_KEY` configured)
- Tests Gemini integration (if `GEMINI_API_KEY` configured)
- Generates summary of test results

**Required Secrets:**
- `OPENAI_API_KEY` (optional)
- `ANTHROPIC_API_KEY` (optional)
- `GEMINI_API_KEY` (optional)

**Usage:**
```bash
# Run specific provider integration tests
gh workflow run integration.yml -f provider=openai
gh workflow run integration.yml -f provider=all
```

---

### 5. Release Workflow (`.github/workflows/release.yml`)

**Triggers:**
- Push tags matching `v*` (e.g., `v1.0.0`)
- Manual dispatch with version input

**Jobs:**

#### Build
- Cross-compiles release binaries for all platforms
- Includes version, commit, and build date in binary
- Creates tarballs for non-Windows platforms

#### Create Release
- Generates changelog from git commits
- Creates GitHub release with binaries
- Marks as prerelease if version contains `alpha`, `beta`, or `rc`

#### Docker
- Builds multi-platform Docker image (amd64, arm64)
- Pushes to GitHub Container Registry (ghcr.io)
- Tags with version and SHA

**Creating a Release:**
```bash
# Tag and push
git tag v1.0.0
git push origin v1.0.0

# Or use GitHub CLI
gh release create v1.0.0 --generate-notes
```

---

## Configuration Files

### `.golangci.yml`

Linter configuration with enabled linters:
- `errcheck` - Unchecked errors
- `gosimple` - Code simplification
- `govet` - Code correctness
- `staticcheck` - Advanced analysis
- `gosec` - Security issues
- `revive` - Fast, flexible linting
- And more...

**Customization:**
Edit `.golangci.yml` to enable/disable linters or adjust settings.

---

## Secrets Configuration

Add these secrets in GitHub repository settings:

**Optional (for integration tests):**
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY`

**Automatic (GitHub provides):**
- `GITHUB_TOKEN` - For release creation, PR comments

---

## Status Badges

Add to README.md:

```markdown
[![CI](https://github.com/byterover/alfred-ai/workflows/CI/badge.svg)](https://github.com/byterover/alfred-ai/actions/workflows/ci.yml)
[![Security](https://github.com/byterover/alfred-ai/workflows/Security%20Scan/badge.svg)](https://github.com/byterover/alfred-ai/actions/workflows/security.yml)
[![Benchmark](https://github.com/byterover/alfred-ai/workflows/Benchmark/badge.svg)](https://github.com/byterover/alfred-ai/actions/workflows/benchmark.yml)
[![codecov](https://codecov.io/gh/byterover/alfred-ai/branch/main/graph/badge.svg)](https://codecov.io/gh/byterover/alfred-ai)
```

---

## Local Testing

Before pushing, run these locally:

```bash
# Run tests
make test

# Run with race detector
make test-race

# Run benchmarks
make bench

# Run linter
golangci-lint run

# Run security scan
gosec ./...

# Check formatting
gofmt -s -l .
```

---

## Troubleshooting

### Race Detector Fails
- Ensure `CGO_ENABLED=1` is set
- Check for concurrent map access without locks
- Review goroutine synchronization

### Benchmark Comparison Fails
- Baseline may not exist yet (first run)
- Benchmark format changed
- Use manual dispatch to regenerate baseline

### Security Scan False Positives
- Exclude specific checks in `.golangci.yml`
- Add inline comments: `// #nosec G104` (with justification)

### Build Artifacts Missing
- Check if job succeeded
- Artifacts expire after 7 days (configurable)
- Re-run workflow to regenerate

---

## Performance Monitoring

Monitor benchmark trends:
1. Check benchmark workflow runs
2. Download artifacts from Actions tab
3. Compare `benchmark-new.txt` files over time
4. Use `benchstat` for statistical comparison

```bash
# Compare two benchmark runs
benchstat old.txt new.txt
```

---

## Best Practices

1. **Never skip CI checks** - They catch real issues
2. **Review security scan results** - Even if informational
3. **Monitor benchmark trends** - Catch performance regressions early
4. **Keep dependencies updated** - Weekly security scans help
5. **Run tests locally first** - Faster feedback loop

---

## Future Enhancements

- [ ] Performance regression detection (auto-fail on >10% slowdown)
- [ ] Slack/Discord notifications for failed builds
- [ ] Automated dependency updates (Dependabot)
- [ ] E2E tests with Docker Compose
- [ ] Deployment to staging environment
- [ ] Load testing in CI

---

For questions or issues with CI/CD, open a GitHub issue with the `ci/cd` label.
