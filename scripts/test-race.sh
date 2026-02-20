#!/bin/bash
# Run tests with race detector
# Requires: CGO_ENABLED=1

set -e

echo "==> Running tests with race detector..."
echo "Note: This requires CGO. If you get 'cgo not enabled', run:"
echo "  CGO_ENABLED=1 ./scripts/test-race.sh"
echo ""

# Enable CGO if not already enabled
export CGO_ENABLED=${CGO_ENABLED:-1}

# Run tests with race detector
go test -race -short ./... "$@"

echo ""
echo "==> Race detection complete. No data races detected."
