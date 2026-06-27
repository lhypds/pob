#!/bin/bash

set -e

echo "Setting up pob MCP server..."

# Check for Python 3
if ! command -v python3 &> /dev/null; then
    echo "Python 3 is not installed. Please install it from https://python.org"
    exit 1
fi

echo "Python found: $(python3 --version)"

# Install dependencies
echo "Installing dependencies..."
pip install mcp

echo ""
echo "Setup complete. To start the server:"
echo "  ./mcp/start.sh"
echo ""
echo "To register with Claude Desktop, add to"
echo "~/Library/Application Support/Claude/claude_desktop_config.json:"
echo ""
echo '  {'
echo '    "mcpServers": {'
echo '      "pob": {'
echo '        "command": "python3",'
echo "        \"args\": [\"$(cd "$(dirname "$0")" && pwd)/pob_mcp_server.py\"]"
echo '      }'
echo '    }'
echo '  }'
