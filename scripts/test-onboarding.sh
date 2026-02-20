#!/bin/bash
# test-onboarding.sh
# Automated test script for alfred-ai onboarding flow
# Tests the Quick Start wizard without requiring real API keys

set -e  # Exit on error

echo "=========================================="
echo "alfred-ai Onboarding Test Suite"
echo "=========================================="
echo ""

# Build the binary first
echo "📦 Building alfred-ai..."
go build -o alfred-ai ./cmd/agent
if [ $? -ne 0 ]; then
    echo "❌ Build failed"
    exit 1
fi
echo "✅ Build successful"
echo ""

# Function to test a specific template path
test_template() {
    local test_name="$1"
    local inputs="$2"

    echo "==========================================
"
    echo "🧪 Testing: $test_name"
    echo "==========================================
"

    # Run setup with piped inputs
    # Capture both stdout and stderr
    output=$(echo -e "$inputs" | ./alfred-ai setup 2>&1 || true)

    # Check for critical errors
    if echo "$output" | grep -qi "panic\|fatal\|segmentation fault"; then
        echo "❌ FAILED: Critical error detected"
        echo "$output" | grep -i "panic\|fatal\|error" | head -10
        return 1
    fi

    # Verify expected messages appear
    local expected_messages=(
        "alfred-ai setup"
        "Quick Start"
        "Advanced Setup"
    )

    for msg in "${expected_messages[@]}"; do
        if ! echo "$output" | grep -q "$msg"; then
            echo "⚠️  Warning: Expected message not found: '$msg'"
        fi
    done

    echo "✅ $test_name completed without crashes"
    echo ""
}

# Test 1: Quick Start - Personal Assistant (with API key skip)
echo "Test 1: Quick Start → Personal Assistant"
# Inputs: 1 (Quick Start), 1 (Personal Assistant), 1 (OpenAI), n (no API key), y (skip), n (no test), y (save)
test_template "Personal Assistant Template" "1\n1\n1\nn\ny\nn\ny\n"

# Test 2: Quick Start - Telegram Bot (with API key skip)
echo "Test 2: Quick Start → Telegram Bot"
# Inputs: 1 (Quick Start), 2 (Telegram), 1 (OpenAI), n (no API key), y (skip), n (no telegram token), y (skip), n (no test), y (save)
test_template "Telegram Bot Template" "1\n2\n1\nn\ny\nn\ny\nn\ny\n"

# Test 3: Quick Start - Secure & Private (with API key skip)
echo "Test 3: Quick Start → Secure & Private"
# Inputs: 1 (Quick Start), 3 (Secure), 1 (OpenAI), n (no API key), y (skip), passphrase, y (continue), workspace (sandbox), ./data/audit.jsonl (audit), n (no test), y (save)
test_template "Secure & Private Template" "1\n3\n1\nn\ny\ntestpass123\ny\n./workspace\n./data/audit.jsonl\nn\ny\n"

# Test 4: Advanced Setup (backward compatibility)
echo "Test 4: Advanced Setup (existing wizard)"
# This tests the old wizard still works
# Inputs: 2 (Advanced), 1 (OpenAI), skip, 2 (markdown), n (no encryption), 1 (CLI), default prompt
test_template "Advanced Setup" "2\n1\n\n2\nn\n1\n\n"

# Summary
echo "=========================================="
echo "📊 Test Summary"
echo "=========================================="
echo "All onboarding tests completed!"
echo ""
echo "✅ Personal Assistant template"
echo "✅ Telegram Bot template"
echo "✅ Secure & Private template"
echo "✅ Advanced Setup (backward compatibility)"
echo ""
echo "Note: These tests use simulated inputs and skip API key validation."
echo "For full validation, run './alfred-ai setup' manually with a real API key."
echo ""
echo "🎉 All tests passed!"
