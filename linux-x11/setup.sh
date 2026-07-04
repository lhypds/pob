#!/bin/bash

# Setup script for the Pob Linux/X11 shell.
# Checks toolchain + library dependencies, then builds the Go core and the shell.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🚀 Setting up Pob (Linux/X11) development environment..."

if ! command -v cc &> /dev/null && ! command -v gcc &> /dev/null; then
    echo "❌ No C compiler found. Install one with:"
    echo "   sudo apt install build-essential      (Debian/Ubuntu)"
    echo "   sudo dnf install gcc make             (Fedora)"
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Install it with your package manager or from https://go.dev/dl/"
    exit 1
fi
echo "✅ Go found: $(go version)"

if ! command -v pkg-config &> /dev/null; then
    echo "❌ pkg-config not found. Install it first."
    exit 1
fi

MISSING=""
for pkg in gtk+-3.0 json-glib-1.0 x11 xtst; do
    if ! pkg-config --exists "$pkg"; then
        MISSING="$MISSING $pkg"
    fi
done

if [ -n "$MISSING" ]; then
    echo "❌ Missing development libraries:$MISSING"
    echo "   Debian/Ubuntu: sudo apt install libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev"
    echo "   Fedora:        sudo dnf install gtk3-devel json-glib-devel libX11-devel libXtst-devel"
    echo "   Arch:          sudo pacman -S gtk3 json-glib libx11 libxtst"
    exit 1
fi
echo "✅ GTK 3, json-glib, X11 and XTest development libraries found"

# Initialize settings.json from example when available
if [ ! -f "$ROOT_DIR/settings.json" ] && [ -f "$ROOT_DIR/settings.json.example" ]; then
    cp "$ROOT_DIR/settings.json.example" "$ROOT_DIR/settings.json"
    echo "✅ Created settings.json from settings.json.example"
fi

echo "🔨 Building core (Go)..."
(cd "$ROOT_DIR/core" && go mod download && go build -o bin/pob-core ./cmd/pob-core)
echo "✅ core build successful"

echo "🔨 Building Linux shell (C/GTK)..."
(cd "$SCRIPT_DIR" && make)
echo "✅ Linux shell build successful"

echo ""
echo "Done. Start the app with ./linux-x11/start.sh (foreground) or ./linux-x11/restart.sh (background)."
