
Key names
=========

`keyPress` / `key_press` takes one key, optionally preceded by `+`-joined
modifiers: `ctrl+alt+shift+f5`. A name is a *position* on the keyboard rather
than a character, so the machine's own layout decides what it produces — which
is what lets the [Web UI](12_Web%20UI.md) and [Pob Keyboard](13_Keyboard.md) forward
real keypresses verbatim. Names are matched in lower case, so `Escape` and
`CMD+V` reach the same keys as `escape` and `cmd+v`.


Modifiers
---------

| Modifier | Aliases | Meaning |
|----------|---------|---------|
| `cmd` | `command` | Command on macOS, Ctrl on Linux and Windows — the ordinary-shortcut modifier, so `cmd+c` copies everywhere |
| `ctrl` | `control` | Control on every platform |
| `alt` | `option`, `opt` | Option / Alt |
| `shift` | | Shift |
| `gui` | `win`, `super`, `meta` | The key beside the space bar itself: Command / Windows / Super |


Letters and digits
------------------

`a`–`z` and `0`–`9`, one character each — the keys on the main block, not the
keypad. Lower case only: `shift` is a modifier, so a capital is `shift+a`.


Named keys
----------

| Key | Aliases | Notes |
|-----|---------|-------|
| `return` | `enter` | The main Return / Enter key. `enter` is the name the macro recorder writes, and a recorded keypad Enter replays as this key too |
| `tab` | | |
| `space` | | |
| `backspace` | `delete` | Deletes backwards — `delete` has always named this key here, and still does |
| `forwarddelete` | | The key that deletes forwards, Del on a PC board |
| `escape` | `esc` | |
| `insert` | | macOS has no Insert key; the name reaches Help, which sits in that position |
| `left`, `right`, `up`, `down` | | The arrow cluster |
| `home`, `end` | | |
| `pageup`, `pagedown` | | |
| `capslock` | | |
| `printscreen` | | F13 on macOS |
| `scrolllock` | | F14 on macOS |
| `pause` | | F15 on macOS |
| `menu` | | Linux and Windows only — an Apple board has no context-menu key |

`printscreen`, `scrolllock` and `pause` are the three keys right of a PC's
function row. An Apple board puts F13–F15 there instead, so on macOS that is
what those names press — the same keycap in the same place.


Function keys
-------------

`f1`–`f24` on Linux and Windows. `f1`–`f20` on macOS, which is as far as its
key codes go; `f21`–`f24` are rejected there.


Punctuation
-----------

Named by the keycap a US layout prints on them.

| Key | US keycap | Key | US keycap |
|-----|-----------|-----|-----------|
| `minus` | `-` | `semicolon` | `;` |
| `equals` | `=` | `quote` | `'` |
| `leftbracket` | `[` | `grave` | `` ` `` |
| `rightbracket` | `]` | `comma` | `,` |
| `backslash` | `\` | `period` | `.` |
| `slash` | `/` | | |

There is no name for a shifted character: `?` is `shift+slash` and `_` is
`shift+minus`. To put text on screen, reach for `typeText` instead — it types any
character on any layout, punctuation and CJK included.


When a name isn't one
---------------------

That is the whole vocabulary — there is nothing for the keypad, and nothing for
the media and volume keys. A name outside the list presses nothing at all and is
written to the [log](05_Logs.md) as `Unknown key: <name>`, which is where to look
when a macro step quietly does nothing.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — `keyPress` in a macro
- [MCP Server](08_MCP.md) — `key_press` as an MCP tool
- [Operation API](10_Operation%20API.md) — `keycode=`, the HID spelling of the same keys
