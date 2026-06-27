#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EXECUTABLE_PATH="$SCRIPT_DIR/.build/debug/Pob"

# Match by executable name first, then by known build-path patterns.
PIDS="$(pgrep -x Pob || true)"

if [ -z "$PIDS" ]; then
    PIDS="$(pgrep -f "$EXECUTABLE_PATH|$SCRIPT_DIR/.build/.*/debug/Pob|/\.build/.*/debug/Pob|/\.build/debug/Pob|/\.build/debug/AII" || true)"
fi

if [ -z "$PIDS" ]; then
    echo "No running Pob process found."
    exit 0
fi

echo "Stopping Pob process: $PIDS"
kill $PIDS

echo "Pob stopped."

# Stop MCP server if running on port 8032
MCP_PIDS="$(lsof -ti tcp:8032 || true)"
if [ -n "$MCP_PIDS" ]; then
    echo "Stopping MCP server: $MCP_PIDS"
    kill $MCP_PIDS
    echo "MCP server stopped."
fi
