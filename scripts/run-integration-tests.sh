#!/bin/bash
set -e

echo "=== Integration Test Runner ==="
echo ""

# Check for API keys
MISSING_KEYS=""
if [ -z "$OPENAI_API_KEY" ]; then
    MISSING_KEYS="${MISSING_KEYS}OPENAI_API_KEY "
fi
if [ -z "$ANTHROPIC_API_KEY" ]; then
    MISSING_KEYS="${MISSING_KEYS}ANTHROPIC_API_KEY "
fi
if [ -z "$GEMINI_API_KEY" ]; then
    MISSING_KEYS="${MISSING_KEYS}GEMINI_API_KEY "
fi

if [ -n "$MISSING_KEYS" ]; then
    echo "⚠️  Warning: Missing API keys: ${MISSING_KEYS}"
    echo "Some tests will be skipped."
    echo ""
fi

# Parse arguments
PROVIDER="${1:-all}"
VERBOSE="${2:-false}"

VERBOSE_FLAG=""
if [ "$VERBOSE" = "true" ] || [ "$VERBOSE" = "-v" ]; then
    VERBOSE_FLAG="-v"
fi

echo "Running integration tests for provider: $PROVIDER"
echo ""

# Run tests
case "$PROVIDER" in
    openai)
        go test -tags=integration $VERBOSE_FLAG -timeout=10m \
            -run "TestOpenAI.*Integration" \
            ./internal/adapter/llm
        ;;
    anthropic)
        go test -tags=integration $VERBOSE_FLAG -timeout=10m \
            -run "TestAnthropic.*Integration" \
            ./internal/adapter/llm
        ;;
    gemini)
        go test -tags=integration $VERBOSE_FLAG -timeout=10m \
            -run "TestGemini.*Integration" \
            ./internal/adapter/llm
        ;;
    multi-tool)
        go test -tags=integration $VERBOSE_FLAG -timeout=15m \
            -run "TestMultiTool" \
            ./internal/adapter/llm
        ;;
    stream)
        go test -tags=integration $VERBOSE_FLAG -timeout=10m \
            -run "TestStream" \
            ./internal/adapter/llm
        ;;
    all)
        go test -tags=integration $VERBOSE_FLAG -timeout=20m \
            ./internal/adapter/llm
        ;;
    *)
        echo "Unknown provider: $PROVIDER"
        echo "Usage: $0 [openai|anthropic|gemini|multi-tool|stream|all] [-v]"
        exit 1
        ;;
esac

echo ""
echo "✓ Integration tests completed"
