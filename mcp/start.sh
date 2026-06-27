#!/bin/bash

set -e

DIR="$(cd "$(dirname "$0")" && pwd)"

# Resolve Python: prefer the pyenv version pinned in .python-version, fall back to python3
if command -v pyenv &> /dev/null && [ -f "$DIR/.python-version" ]; then
    PYENV_VERSION=$(cat "$DIR/.python-version")
    PYTHON="$(pyenv root)/versions/${PYENV_VERSION}/bin/python3"
else
    PYTHON="python3"
fi

echo "Starting pob MCP server..."
"$PYTHON" "$DIR/pob_mcp_server.py" --sse "$@"
