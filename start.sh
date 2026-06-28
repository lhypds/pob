#!/bin/bash

# Run script for Pob project
# Builds and runs the macOS desktop application

set -e

echo "🔨 Building..."
swift build

EXECUTABLE_PATH=".build/debug/Pob"

echo "▶️  Launching Pob..."
"$EXECUTABLE_PATH"
