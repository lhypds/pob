#!/bin/bash

# Run script for Pob project
# Builds the Go core + macOS shell and runs the app in the foreground

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "🔨 Building core (Go)..."
(cd "$SCRIPT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR/macos" && swift build)

echo "▶️  Launching Pob..."
cd "$SCRIPT_DIR"
"$SCRIPT_DIR/macos/.build/debug/Pob"
