#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "🔨 Building..."
cd "$SCRIPT_DIR" && swift build

"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "▶️  Launching Pob..."
nohup "$SCRIPT_DIR/.build/debug/Pob" >"$SCRIPT_DIR/app.log" 2>&1 &
echo "Pob restarted in background. Logs: $SCRIPT_DIR/app.log"
exit 0
