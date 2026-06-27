#!/bin/bash

set -e

REQUIRED_PYTHON="3.13.0"
REQUIRED_MINOR=10  # mcp requires 3.10+

DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Setting up pob MCP server..."

# Check for pyenv
if ! command -v pyenv &> /dev/null; then
    echo "pyenv is not installed. Install it with:"
    echo "  brew install pyenv"
    echo "  echo 'eval \"\$(pyenv init -)\"' >> ~/.zshrc"
    echo "  source ~/.zshrc"
    exit 1
fi

echo "pyenv found: $(pyenv --version)"

# Install Python via pyenv if not already installed
if ! pyenv versions --bare | grep -q "^${REQUIRED_PYTHON}$"; then
    echo "Installing Python ${REQUIRED_PYTHON} via pyenv..."
    pyenv install "$REQUIRED_PYTHON"
fi

# Set local Python version for this directory
pyenv local "$REQUIRED_PYTHON"

# Use the explicit pyenv binary so we don't rely on shims being active
PYENV_PYTHON="$(pyenv root)/versions/${REQUIRED_PYTHON}/bin/python3"
echo "Python set to: $($PYENV_PYTHON --version)"

# Install dependencies
echo "Installing dependencies..."
"$PYENV_PYTHON" -m pip install mcp

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
echo "        \"args\": [\"$DIR/pob_mcp_server.py\"]"
echo '      }'
echo '    }'
echo '  }'
