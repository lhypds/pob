#!/bin/bash

# Builds the Go core + macOS shell and launches Pob in the background.
# Run it again — or pass a count — to start additional instances side by side.
#
# Usage: ./start.sh [count]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

COUNT="${1:-1}"
case "$COUNT" in
    ''|*[!0-9]*|0) echo "Usage: $0 [count]  (count must be a positive number)"; exit 1 ;;
esac

echo "🔨 Building core (Go)..."
VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo dev)"
(cd "$ROOT_DIR/core" && go build -o bin/pob-core ./cmd/pob-core && go build -ldflags="-X main.version=$VERSION" -o bin/pob ./cmd/pob)

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR" && swift build)

# Crash output and runtime warnings go where every component's log already
# goes — ~/.pob/app.log, the file the toolbar's App Log button opens and the
# one `pob` uses when it launches the app itself. A run leaves the checkout
# untouched.
APP_LOG="$HOME/.pob/app.log"
mkdir -p "$HOME/.pob"

cd "$ROOT_DIR"
for _ in $(seq "$COUNT"); do
    nohup "$SCRIPT_DIR/.build/debug/Pob" >>"$APP_LOG" 2>&1 &
    echo "▶️  Pob started (pid $!)."
done
echo "Logs: $APP_LOG — stop all instances with ./stop.sh"
