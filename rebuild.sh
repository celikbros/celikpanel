#!/bin/bash

# CelikPanel Restart Script
# This script rebuilds and restarts both agent and panel

set -e

echo "🔨 Building CelikPanel..."

# Build agent
echo "  → Building agent..."
go build -o bin/agent ./cmd/agent

# Build panel
echo "  → Building panel..."
go build -o bin/panel ./cmd/panel

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
