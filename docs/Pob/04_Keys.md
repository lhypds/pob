
Key names
=========

`keyPress` / `key_press` takes one key, optionally preceded by `+`-joined
modifiers: `ctrl+alt+shift+f5`. A name is a *position* on the keyboard rather
than a character, so the machine's own layout decides what it produces — which
is what lets the [Web UI](12_Web%20UI.md) and [Pob Keyboard](13_Keyboard.md) forward
real keypresses verbatim. Names are matched in lower case, so `Escape` and
`CMD+V` reach the same keys as `escape` and `cmd+v`. A key written as a single
[character](#characters) is the other way round: it presses whatever key
produces that character here.

`+` is the separator and a key both, told apart by where it sits: `+` on its own
is the plus key, and `cmd++` holds Command over it.


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

These are positions, so `slash` presses that key whatever the layout prints on
it. For the character rather than the position, write the character.


Characters
----------

A key written as a single character — `*`, `=`, `+`, `?`, `%` — presses whatever
key produces that character on the machine's own layout, holding Shift or Option
along the way if that is how the character is reached there. `*` is Shift and
the `8` key on a US board and its own key on a French one; either way the app
sees a `*`.

This is the question a calculator or a form asks: the button says `×` but the
key that works it is `*`, and `keyPress("*")` is how that is written. `×` and
`÷` are signs a screen prints rather than keys a board has — no layout produces
them, so they resolve to nothing and the call fails.

Characters are matched as written, since case is what tells `%` from `5`. Names
win where the two overlap: `a` is the letter key, not a character lookup, and
`shift+a` is still how a capital is pressed.

To put a run of text on screen, reach for `typeText` instead — it types any
character on any layout, punctuation and CJK included, without a key having to
exist for it.


When a key isn't one
--------------------

That is the whole vocabulary — there is nothing for the keypad, and nothing for
the media and volume keys. A key outside it presses nothing, and *says so*: the
call fails, the reason is written to the [log](05_Logs.md) as
`Unknown key: <name>`, and it reaches whatever asked for the press — an MCP
client gets an error back, a macro writes `keyPress("×") failed` under the step.
A key nobody could resolve used to be logged and shrugged off while the caller
was told it had been pressed, which is how a macro doing half of what it says
comes back looking like a clean run.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — `keyPress` in a macro
- [MCP Server](08_MCP.md) — `key_press` as an MCP tool
- [Operation API](10_Operation%20API.md) — `keycode=`, the HID spelling of the same keys
