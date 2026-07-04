#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "🔨 Building core (Go)..."
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR" && swift build)

"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "▶️  Launching Pob..."
cd "$ROOT_DIR"
nohup "$SCRIPT_DIR/.build/debug/Pob" >"$ROOT_DIR/app.log" 2>&1 &
echo "Pob restarted in background. Logs: $ROOT_DIR/app.log"
exit 0
