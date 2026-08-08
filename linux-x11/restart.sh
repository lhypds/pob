#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "🔨 Building core (Go)..."
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core && go build -o bin/pob ./cmd/pob)

echo "🔨 Building Linux shell (C/GTK)..."
(cd "$SCRIPT_DIR" && make)

"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "▶️  Launching Pob..."
# Appended to ~/.pob/app.log, next to what the app and the core log there
# themselves — see linux-x11/start.sh.
APP_LOG="$HOME/.pob/app.log"
mkdir -p "$HOME/.pob"
cd "$ROOT_DIR"
nohup "$SCRIPT_DIR/bin/pob" >>"$APP_LOG" 2>&1 &
echo "Pob restarted in background. Logs: $APP_LOG"
exit 0
