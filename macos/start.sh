#!/bin/bash

# Builds the Go core + macOS shell and runs the app in the foreground.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🔨 Building core (Go)..."
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR" && swift build)

echo "▶️  Launching Pob..."
cd "$ROOT_DIR"
"$SCRIPT_DIR/.build/debug/Pob"
