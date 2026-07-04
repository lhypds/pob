#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "🔨 Building core (Go)..."
(cd "$SCRIPT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR/macos" && swift build)

"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "▶️  Launching Pob..."
cd "$SCRIPT_DIR"
nohup "$SCRIPT_DIR/macos/.build/debug/Pob" >"$SCRIPT_DIR/app.log" 2>&1 &
echo "Pob restarted in background. Logs: $SCRIPT_DIR/app.log"
exit 0
