#!/bin/bash

# Setup script for Pob project
# This script configures the development environment for the macOS desktop app

set -e

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

# Build the project
echo "🔨 Building the project..."
swift build

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo ""
    echo "📝 Next steps:"
    echo "   1. Run: ./start.sh"
    echo "   2. Open Settings and add your OpenAI API key"
    echo "   3. Test the connection"
    echo ""
else
    echo "❌ Build failed"
    exit 1
fi
