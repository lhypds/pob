#!/bin/bash

# Setup script for Pob project
# This script configures the development environment for the macOS desktop app

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "🚀 Setting up Pob development environment..."

# Check for Swift
if ! command -v swift &> /dev/null; then
    echo "❌ Swift is not installed. Please install Xcode Command Line Tools:"
    echo "   xcode-select --install"
    exit 1
fi

echo "✅ Swift found: $(swift --version)"

# Create necessary directories
echo "📁 Creating necessary directories..."
mkdir -p Sources
mkdir -p ~/.config/Pob

# Verify Package.swift exists
if [ ! -f "Package.swift" ]; then
    echo "❌ Package.swift not found in current directory"
    exit 1
fi

echo "✅ Package.swift found"

# Initialize .env from template when available
if [ ! -f ".env" ] && [ -f ".env.example" ]; then
    cp .env.example .env
    echo "✅ Created .env from .env.example"
fi

# Initialize settings.json from example when available
if [ ! -f "settings.json" ] && [ -f "settings.json.example" ]; then
    cp settings.json.example settings.json
    echo "✅ Created settings.json from settings.json.example"
fi

# Build the project
echo "🔨 Building the project..."
swift build

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
else
    echo "❌ Build failed"
    exit 1
fi

# Set up MCP server
echo ""
echo "🔌 Setting up MCP server..."
"$SCRIPT_DIR/mcp/setup.sh"
