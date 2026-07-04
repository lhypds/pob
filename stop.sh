#!/bin/bash

# Dispatches to the OS shell recorded in the SYSTEM file (see ./setup.sh).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SYSTEM_FILE="$SCRIPT_DIR/SYSTEM"

if [ ! -f "$SYSTEM_FILE" ]; then
    echo "❌ No SYSTEM file found — run ./setup.sh first."
    exit 1
fi

SYSTEM="$(tr -d '[:space:]' < "$SYSTEM_FILE")"

if [ ! -f "$SCRIPT_DIR/$SYSTEM/stop.sh" ]; then
    echo "❌ Unknown SYSTEM '$SYSTEM' — run ./setup.sh again."
    exit 1
fi

exec "$SCRIPT_DIR/$SYSTEM/stop.sh" "$@"
