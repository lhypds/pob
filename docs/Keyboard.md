
Pob Keyboard
============

A desktop client for the same API: a full-size 104-key board and a trackpad in
one window, for driving a machine running Pob from another computer.

```
./keyboard.sh
./keyboard.sh -url http://192.168.1.40:8033/pb-3f9a
```

With no address it opens Settings… straight away, laid out as the address
itself — machine, port, instance ID — and remembered between runs. Pasting the
whole line `pob status` prints into the first field fills all three.

Keys pressed on your real keyboard are forwarded too while the window has
focus, and light up the matching keycap. Modifier keys latch — click once for
the next key only, twice to lock — so a shortcut can be built without holding
anything down. The **Target** setting (Windows / macOS) doesn't change what
gets sent, only how the keys either side of the space bar are labelled and
ordered.

It is the pico-hid board's keyboard client pointed at Pob instead: the same
board, the same trackpad, the same wire protocol. Only the address differs.

Building it needs a C compiler (the UI draws through OpenGL): `xcode-select
--install` on macOS, `sudo apt install gcc libgl1-mesa-dev xorg-dev` on
Debian/Ubuntu. It is its own Go module in `keyboard/`, not built by the root
scripts.


See also
--------

- [Pob Server](Server.md) — what it talks to, and the address to give it
- [Pob Server API](API.md) — the protocol it speaks
- [Key names](Keys.md) — what the keys resolve to
- [Web UI](WebUI.md) — the same thing in a browser
