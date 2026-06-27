#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

"$SCRIPT_DIR/stop.sh"

echo "Starting Pob..."
nohup "$SCRIPT_DIR/start.sh" >/tmp/pob-restart.log 2>&1 &
echo "Pob restarted in background. Logs: /tmp/pob-restart.log"
exit 0
