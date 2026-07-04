#!/bin/bash

# Builds the Go core + Linux/X11 shell and runs the app in the foreground.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🔨 Building core (Go)..."
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core)

echo "🔨 Building Linux shell (C/GTK)..."
(cd "$SCRIPT_DIR" && make)

echo "▶️  Launching Pob..."
cd "$ROOT_DIR"
exec "$SCRIPT_DIR/bin/pob"
