#!/bin/bash

# Run script for Pob project
# Builds and runs the macOS desktop application

set -e

echo "🚀 Starting Pob..."

# Check if build is needed
if [ ! -d ".build" ]; then
    echo "🔨 Build directory not found, building..."
    swift build
fi

# Run the application
EXECUTABLE_PATH=".build/debug/Pob"

if [ ! -f "$EXECUTABLE_PATH" ]; then
    echo "❌ Executable not found at $EXECUTABLE_PATH"
    echo "🔨 Building..."
    swift build
fi

echo "▶️  Launching Pob..."
"$EXECUTABLE_PATH"
