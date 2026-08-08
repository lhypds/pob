
Key names
=========

`keyPress` / `key_press` takes one key, optionally preceded by `+`-joined
modifiers: `ctrl+alt+shift+f5`. A name is a *position* on the keyboard rather
than a character, so the machine's own layout decides what it produces — which
is what lets the [Web UI](WebUI.md) and [Pob Keyboard](Keyboard.md) forward
real keypresses verbatim.

| Modifier | Meaning |
|----------|---------|
| `cmd` | Command on macOS, Ctrl on Linux and Windows — the ordinary-shortcut modifier, so `cmd+c` copies everywhere |
| `ctrl` | Control on every platform |
| `alt` | Option / Alt |
| `shift` | Shift |
| `gui` | The key beside the space bar itself: Command / Windows / Super (aliases: `win`, `super`, `meta`) |

Keys: `a`–`z`, `0`–`9`, `return`, `tab`, `space`, `backspace`,
`forwarddelete`, `escape`, `insert`, `left`, `right`, `up`, `down`, `home`,
`end`, `pageup`, `pagedown`, `capslock`, `printscreen`, `scrolllock`, `pause`,
`menu`, `f1`–`f24`, `minus`, `equals`, `leftbracket`, `rightbracket`,
`backslash`, `semicolon`, `quote`, `grave`, `comma`, `period`, `slash`.

`delete` is an alias for `backspace`, as it always was; the key that deletes
forwards is `forwarddelete`. macOS has no `menu` key, and takes `printscreen`,
`scrolllock` and `pause` to mean F13–F15 — the same keycaps in the same place
on an Apple board.


See also
--------

- [Macro](Macro.md) — `keyPress` in macros and AI sessions
- [MCP Server](MCP.md) — `key_press` as an MCP tool
- [Pob Server API](API.md) — `keycode=`, the HID spelling of the same keys
