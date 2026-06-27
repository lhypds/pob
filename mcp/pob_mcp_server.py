#!/usr/bin/env python3
"""
pob_mcp_server — MCP server that exposes screen capture to AI applications.

Install dependency:
    pip install mcp

Stdio mode (started by Claude Desktop):
    python3 pob_mcp_server.py

SSE mode (started by Pob app, or run manually for persistent access):
    python3 pob_mcp_server.py --sse          # port 8032
    python3 pob_mcp_server.py --sse --port 9000

Claude Desktop — stdio config:
    {
      "mcpServers": {
        "pob": {
          "command": "python3",
          "args": ["/path/to/pob/mcp/pob_mcp_server.py"]
        }
      }
    }

Claude Desktop — SSE config (when Pob auto-starts the server):
    {
      "mcpServers": {
        "pob": {
          "url": "http://localhost:8032/sse"
        }
      }
    }
"""

import base64
import os
import subprocess
import sys
import tempfile
from typing import Optional

import mcp.types as types
from mcp.server.fastmcp import FastMCP


def _parse_port(args: list[str], default: int) -> int:
    if "--port" in args:
        idx = args.index("--port")
        if idx + 1 < len(args):
            return int(args[idx + 1])
    return default


def _make_server(host: str = "127.0.0.1", port: int = 8032) -> FastMCP:
    return FastMCP("pob", host=host, port=port)


mcp = _make_server()


@mcp.tool()
def take_screenshot(
    crop_x: Optional[int] = None,
    crop_y: Optional[int] = None,
    crop_width: Optional[int] = None,
    crop_height: Optional[int] = None,
) -> list[types.ImageContent]:
    """
    Capture a screenshot of the primary display and return it as a PNG image.

    All crop parameters are optional. When all four are provided, only that
    region of the screen is captured. Coordinates are in screen points
    (logical pixels), origin at the top-left of the primary display.

    Args:
        crop_x: Left edge of the capture region in screen points.
        crop_y: Top edge of the capture region in screen points.
        crop_width: Width of the capture region in screen points.
        crop_height: Height of the capture region in screen points.
    """
    fd, tmpfile = tempfile.mkstemp(suffix=".png")
    os.close(fd)

    try:
        cmd = ["screencapture", "-x", "-t", "png"]
        if all(v is not None for v in [crop_x, crop_y, crop_width, crop_height]):
            cmd += ["-R", f"{crop_x},{crop_y},{crop_width},{crop_height}"]
        cmd.append(tmpfile)
        subprocess.run(cmd, check=True)

        with open(tmpfile, "rb") as f:
            data = base64.standard_b64encode(f.read()).decode()
    finally:
        os.unlink(tmpfile)

    return [types.ImageContent(type="image", data=data, mimeType="image/png")]


if __name__ == "__main__":
    args = sys.argv[1:]
    if "--sse" in args:
        port = _parse_port(args, 8032)
        server = _make_server(host="127.0.0.1", port=port)
        # Re-register tools on the new server instance
        server.add_tool(take_screenshot)
        print(f"pob MCP server started (SSE on http://127.0.0.1:{port})", flush=True)
        server.run(transport="sse")
    else:
        print("pob MCP server started (stdio)", flush=True)
        mcp.run(transport="stdio")
