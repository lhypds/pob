#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "🔨 Building core (Go)..."
VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo dev)"
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core && go build -ldflags="-X main.version=$VERSION" -o bin/pob ./cmd/pob)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR" && swift build)

"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "▶️  Launching Pob..."
# Appended to ~/.pob/app.log, next to what the app and the core log there
# themselves — see macos/start.sh.
APP_LOG="$HOME/.pob/app.log"
mkdir -p "$HOME/.pob"
cd "$ROOT_DIR"
nohup "$SCRIPT_DIR/.build/debug/Pob" >>"$APP_LOG" 2>&1 &
echo "Pob restarted in background. Logs: $APP_LOG"
exit 0
