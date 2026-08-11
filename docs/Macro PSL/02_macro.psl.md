
macro.psl
=========

Each instance has one macro, `macro.psl`, in its `~/.pob/<instance>/` directory. The Macro PSL
button (🪄) in the toolbar opens it in your editor; Execute (▶) checks it and replays it; Reset (↻)
empties it.

Use the record button (⏺) to write it instead of typing it — actions are appended to `macro.psl` as
they happen. Starting a recording while `macro.psl` still holds statements asks what to do with them
first: clear them, or keep them and record after them. Keeping them writes a `resetCursor()` between
the old lines and the new ones, since every move recorded next is relative to the origin a replay
starts at.

Recording captures every action that drives the machine, whichever one of the three is driving it:
your own mouse and keyboard, the AI's tool calls, and the tools an [MCP](../Pob/08_MCP.md) client calls.
They all append to the same `macro.psl`, in the order things happened. The MCP tools that take an
absolute `(x, y)` are written down as the relative `move(dx, dy)` this vocabulary replays, so a
recording made through MCP plays back like any other.

Your own mouse and keyboard are recorded on macOS only, for now — watching the input of other
applications is a different mechanism on each system, and the Linux and Windows shells do not have
it yet. On those two the record button still captures everything the AI and MCP clients drive.

A macro recorded before the file was named `macro.psl` is carried over from `macro.txt` the first
time this Pob runs, so nothing that was recorded is lost to the rename.


See also
--------

- [Structure](03_Structure.md) — what the lines in the file are
- [UI](../Pob/02_UI.md) — the Macro PSL, record and Execute buttons
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools, recorded into the same file
- [Logs](../Pob/05_Logs.md) — the session a run writes
