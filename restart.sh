#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "Starting Pob..."
nohup "$SCRIPT_DIR/start.sh" >"$SCRIPT_DIR/app.log" 2>&1 &
echo "Pob restarted in background. Logs: $SCRIPT_DIR/app.log"
exit 0
