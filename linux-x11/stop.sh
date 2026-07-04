#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

PIDS="$(pgrep -f "$SCRIPT_DIR/bin/pob\$" || true)"

# The Go core exits on its own when the shell dies (stdin EOF), but clean up
# any stragglers.
CORE_PIDS="$(pgrep -x pob-core || true)"

if [ -z "$PIDS" ] && [ -z "$CORE_PIDS" ]; then
    echo "No running Pob process found."
    exit 0
fi

if [ -n "$PIDS" ]; then
    echo "Stopping Pob process: $PIDS"
    kill $PIDS
fi

if [ -n "$CORE_PIDS" ]; then
    echo "Stopping pob-core process: $CORE_PIDS"
    kill $CORE_PIDS 2>/dev/null || true
fi

echo "Pob stopped."
