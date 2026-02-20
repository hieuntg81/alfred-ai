# Makefile for alfred-ai

.PHONY: help test test-race bench bench-compare fuzz fuzz-short clean build build-edge build-iot-arm64 build-iot-arm

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

test: ## Run all tests
	go test ./...

test-verbose: ## Run tests with verbose output
	go test -v ./...

test-race: ## Run tests with race detector (requires CGO)
	CGO_ENABLED=1 go test -race ./...

test-cover: ## Run tests with coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Fuzzing targets
fuzz-short: ## Quick fuzz regression (10s per test)
	@echo "Running quick fuzz tests..."
	go test -fuzz=FuzzShellTool -fuzztime=10s ./internal/adapter/tool
	go test -fuzz=FuzzFilesystemTool -fuzztime=10s ./internal/adapter/tool
	go test -fuzz=FuzzWebTool -fuzztime=10s ./internal/adapter/tool

fuzz-shell: ## Fuzz shell tool (5min)
	go test -fuzz=FuzzShellTool -fuzztime=5m ./internal/adapter/tool

fuzz-filesystem: ## Fuzz filesystem tool (5min)
	go test -fuzz=FuzzFilesystemTool -fuzztime=5m ./internal/adapter/tool
	go test -fuzz=FuzzFilesystemTOCTOU -fuzztime=5m ./internal/adapter/tool

fuzz-web: ## Fuzz web tool (5min)
	go test -fuzz=FuzzWebTool -fuzztime=5m ./internal/adapter/tool

fuzz-all: ## Run all fuzz tests (30min)
	@echo "Running comprehensive fuzz tests..."
	go test -fuzz=. -fuzztime=30m ./internal/adapter/tool

fuzz-minimize: ## Minimize corpus
	go test -run=^$$ -fuzz=FuzzShellTool -fuzzminimizetime=1m ./internal/adapter/tool
	go test -run=^$$ -fuzz=FuzzFilesystemTool -fuzzminimizetime=1m ./internal/adapter/tool

## Integration Testing Targets

.PHONY: test-integration test-integration-llm test-integration-channels test-integration-e2e test-integration-all test-integration-check

test-integration: ## Run all integration tests (requires API keys)
	@echo "Running integration tests (requires API keys in environment)..."
	go test -v -tags=integration ./internal/integration/... ./internal/adapter/llm/... ./internal/adapter/channel/...

test-integration-llm: ## Run LLM provider integration tests
	@echo "Testing OpenAI integration..."
	@test -n "$$OPENAI_API_KEY" || (echo "OPENAI_API_KEY not set" && exit 1)
	go test -v -tags=integration -run TestOpenAI ./internal/adapter/llm/
	@echo ""
	@echo "Testing Anthropic integration..."
	@test -n "$$ANTHROPIC_API_KEY" || (echo "ANTHROPIC_API_KEY not set" && exit 1)
	go test -v -tags=integration -run TestAnthropic ./internal/adapter/llm/
	@echo ""
	@echo "Testing Gemini integration..."
	@test -n "$$GEMINI_API_KEY" || (echo "GEMINI_API_KEY not set" && exit 1)
	go test -v -tags=integration -run TestGemini ./internal/adapter/llm/

test-integration-channels: ## Run channel integration tests
	go test -v -tags=integration ./internal/adapter/channel/

test-integration-e2e: ## Run end-to-end integration tests
	go test -v -tags=integration -timeout=5m ./internal/integration/

test-integration-all: ## Run all integration tests with coverage
	go test -v -tags=integration -cover -coverprofile=coverage_integration.out ./internal/integration/... ./internal/adapter/...
	go tool cover -html=coverage_integration.out -o coverage_integration.html

test-integration-check: ## Check if integration test credentials are configured
	@echo "Checking integration test credentials..."
	@test -n "$$OPENAI_API_KEY" && echo "✓ OPENAI_API_KEY set" || echo "✗ OPENAI_API_KEY not set"
	@test -n "$$ANTHROPIC_API_KEY" && echo "✓ ANTHROPIC_API_KEY set" || echo "✗ ANTHROPIC_API_KEY not set"
	@test -n "$$GEMINI_API_KEY" && echo "✓ GEMINI_API_KEY set" || echo "✗ GEMINI_API_KEY not set"
	@test -n "$$TELEGRAM_BOT_TOKEN" && echo "✓ TELEGRAM_BOT_TOKEN set" || echo "✗ TELEGRAM_BOT_TOKEN not set"

bench: ## Run all benchmarks
	go test -bench=. -benchmem ./...

bench-agent: ## Run agent benchmarks
	go test -bench=BenchmarkAgent -benchmem ./internal/usecase

bench-eventbus: ## Run event bus benchmarks
	go test -bench=BenchmarkEventBus -benchmem ./internal/usecase/eventbus

bench-session: ## Run session benchmarks
	go test -bench=BenchmarkSession -benchmem ./internal/usecase

bench-compare: ## Run benchmarks and save for comparison
	@echo "Running benchmarks and saving to bench-current.txt..."
	go test -bench=. -benchmem ./... | tee bench-current.txt
	@echo ""
	@echo "To compare with previous run:"
	@echo "  1. Save current: mv bench-current.txt bench-old.txt"
	@echo "  2. Make changes"
	@echo "  3. Run: make bench-compare"
	@echo "  4. Compare: benchstat bench-old.txt bench-current.txt"

bench-cpu: ## Run benchmarks with CPU profiling
	go test -bench=BenchmarkAgent -benchmem -cpuprofile=cpu.prof ./internal/usecase
	@echo "CPU profile saved to cpu.prof"
	@echo "View with: go tool pprof cpu.prof"

bench-mem: ## Run benchmarks with memory profiling
	go test -bench=BenchmarkAgent -benchmem -memprofile=mem.prof ./internal/usecase
	@echo "Memory profile saved to mem.prof"
	@echo "View with: go tool pprof mem.prof"

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

fmt: ## Format code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

build: ## Build the binary
	go build -o bin/alfred-ai ./cmd/agent

build-edge: ## Build edge binary (lightweight, IoT-friendly)
	CGO_ENABLED=0 go build -tags edge -ldflags="-s -w" -o bin/alfred-edge ./cmd/agent

build-iot-arm64: ## Build edge binary for ARM64 (Raspberry Pi 4+, Jetson)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags edge -ldflags="-s -w" -o bin/alfred-iot-arm64 ./cmd/agent

build-iot-arm: ## Build edge binary for ARMv7 (Raspberry Pi 3, older)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -tags edge -ldflags="-s -w" -o bin/alfred-iot-arm ./cmd/agent

clean: ## Clean build artifacts
	rm -f coverage.out coverage.html
	rm -f cpu.prof mem.prof
	rm -f bench-*.txt
	rm -rf bin/

.DEFAULT_GOAL := help
