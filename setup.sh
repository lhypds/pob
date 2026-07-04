#!/bin/bash

# Setup script for Pob project
# Configures the development environment: Go core + macOS Swift shell

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "🚀 Setting up Pob development environment..."

# Check for Swift
if ! command -v swift &> /dev/null; then
    echo "❌ Swift is not installed. Please install Xcode Command Line Tools:"
    echo "   xcode-select --install"
    exit 1
fi

echo "✅ Swift found: $(swift --version | head -1)"

# Check for Go
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Install it with Homebrew:"
    echo "   brew install go"
    echo "   (or download from https://go.dev/dl/)"
    exit 1
fi

echo "✅ Go found: $(go version)"

# Verify project layout
if [ ! -f "$SCRIPT_DIR/core/go.mod" ]; then
    echo "❌ core/go.mod not found — is this the project root?"
    exit 1
fi

if [ ! -f "$SCRIPT_DIR/macos/Package.swift" ]; then
    echo "❌ macos/Package.swift not found — is this the project root?"
    exit 1
fi

# Initialize settings.json from example when available
if [ ! -f "$SCRIPT_DIR/settings.json" ] && [ -f "$SCRIPT_DIR/settings.json.example" ]; then
    cp "$SCRIPT_DIR/settings.json.example" "$SCRIPT_DIR/settings.json"
    echo "✅ Created settings.json from settings.json.example"
fi

# Download Go module dependencies (currently none — stdlib only) and build
echo "🔨 Building core (Go)..."
(cd "$SCRIPT_DIR/core" && go mod download && go build -o bin/pob-core ./cmd/pob-core)
echo "✅ core build successful"

echo "🔨 Building macOS shell (Swift)..."
(cd "$SCRIPT_DIR/macos" && swift build)
echo "✅ macOS shell build successful"

echo ""
echo "Done. Start the app with ./start.sh (foreground) or ./restart.sh (background)."
