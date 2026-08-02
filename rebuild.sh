#!/bin/bash

# CelikPanel Restart Script
# This script rebuilds and restarts both agent and panel

set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT"

REQUIRED_GO_VERSION=go1.26.5
if [[ -x "$ROOT/.bin/go/bin/go" ]]; then
    GO_BIN="$ROOT/.bin/go/bin/go"
else
    GO_BIN=$(command -v go 2>/dev/null || true)
fi
[[ -n "$GO_BIN" ]] || {
    echo "Go $REQUIRED_GO_VERSION is required; go was not found" >&2
    exit 1
}
run_go_clean() {
    env -i \
        HOME="${HOME:-/root}" \
        PATH="$PATH" \
        LC_ALL=C \
        GOTOOLCHAIN=local \
        GOENV=off \
        GOWORK=off \
        CGO_ENABLED=0 \
        "$@"
}

actual_go_version=$(run_go_clean "$GO_BIN" env GOVERSION 2>/dev/null || true)
[[ "$actual_go_version" == "$REQUIRED_GO_VERSION" ]] || {
    echo "Go $REQUIRED_GO_VERSION is required; found ${actual_go_version:-unreadable} at $GO_BIN" >&2
    exit 1
}

echo "🔨 Building CelikPanel..."

# Build agent
echo "  → Building agent..."
run_go_clean "$GO_BIN" build -o bin/agent ./cmd/agent

# Build panel
echo "  → Building panel..."
run_go_clean "$GO_BIN" build -o bin/panel ./cmd/panel

echo "✅ Build complete!"
echo ""
echo "🔄 To restart the services, run:"
echo ""
echo "  Terminal 1 (Agent):"
echo "    sudo ./bin/agent"
echo ""
echo "  Terminal 2 (Panel):"
echo "    ./bin/panel"
echo ""
