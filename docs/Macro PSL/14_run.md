run
===

`run(command)` hands one command line to this machine's shell where it stands:

```
once (:: a new message is on screen ::) {
    run("afplay /System/Library/Sounds/Morse.aiff")
}
```

That is the whole of it, and it is the vocabulary's one way out of the window. Every other statement
is the screen — a position on it, a click into it, a key its focus receives — and this one is the
machine underneath: a sound played, a file moved, a script the macro has no other way of asking for.

What goes in the quotes is a command line rather than a program and a list of arguments, so the
shell's own quoting, pipes, redirections and `&` are all there and mean what they always did:

```
run("afplay ~/sounds/done.wav")
run("say \"the upload finished\"")
run("cp ~/Downloads/report.pdf ~/Documents/")
run("curl -fsS https://example.com/ping > /dev/null")
```

The shell is `/bin/sh` on macOS and Linux — the one a `stop_hook` goes to, see
[Settings](../Pob/06_Settings.md) — and `cmd` on Windows. A macro written to be replayed on more than one of them
says what it means in a shell all of them have, or asks the same thing three ways under an
[`if`](07_if%20blocks.md).

The command that plays a sound is the machine's own, and it is what the three platforms differ on:

| | Written |
|-|---------|
| macOS | `run("afplay /Users/me/sounds/done.wav")` |
| Linux | `run("paplay /home/me/sounds/done.wav")` — or `aplay`, where there is no PulseAudio |
| Windows | `run("powershell -c (New-Object Media.SoundPlayer 'C:\\sounds\\done.wav').PlaySync()")` |

The backslashes are doubled because a backslash escapes the character after it inside a PSL string —
see [Calls](06_Calls.md) — so `\\` is the one backslash the command is handed.


Paths
-----

Write them out in full. A `run` is a statement in a file that is replayed from `src/` by an app
started from wherever the machine starts it, and a bare `sound.wav` in it is a guess about which
directory that is. `/Users/me/sounds/done.wav` is not.

A relative path does have an answer, and it is the one [`call`](11_call.md) gives: the command is run
from the directory the PSL file is in, so `run("afplay sounds/done.wav")` in
`~/.pob/<instance>/src/main.macro.psl` plays `~/.pob/<instance>/src/sounds/done.wav`. That is worth
knowing and worth not relying on — a macro that is worth keeping is worth an absolute path, and `~/`
is the shell's own way of writing one that travels between machines.


Waiting
-------

The statement waits for the command. That is what lets the statement under it be written after it —
the sound has played, the file is now there, the script has done whatever the next click depends on
— and it is why a `run` is a statement rather than a thing a macro sets off and hopes about.

A command that should outlive the statement says so in the shell's own words:

```
run("afplay ~/sounds/long.wav &")     // comes back at once, and goes on playing
```

A command still running after a minute is stopped, and the log says so. A minute is far longer than
anything a line of a macro waits for on purpose; what usually hits it is a command reading from a
terminal nobody is at — an editor, a password prompt, a `y/n` — and a run held there with nothing on
screen saying why is the one thing worse than a statement that failed. The command reads from the
null device for that same reason, so a question it asks is a question it does not get an answer to.

Stop ends a `run` the way it ends a `sleep`: the command goes with the run rather than outliving it.


What the log says
-----------------

The statement, then what the command printed — both streams, folded onto the one line an entry is
and cut short at 200 characters:

```
Macro run("afplay /System/Library/Sounds/Morse.aiff")
Macro run("git -C ~/notes pull") — it said: Already up to date.
```

A command that exits non-zero did not do its thing, so its step ends `failed` rather than
`completed`, with what it printed beside it. The replay carries on to the next statement either way,
the way it does past every statement it could not carry out — see
[When something is wrong](12_When%20something%20is%20wrong.md).


What it is not
--------------

It is not a way of doing on the command line what the macro is written to do on screen. The point of
a macro is the window: an app with no scripting behind it, driven the way a person drives it. `run`
is for the things that are not in that window at all.

It is also not checked, past the name and the one argument. The check reads a macro as PSL and a
command line is not PSL — `run("rm -rf ~/notes")` is a statement that is written correctly. A file
that arrived from somewhere else is worth reading before it is played, and the `run` lines are the
ones to read first.


See also
--------

- [Calls](06_Calls.md) — every statement, and the three things a call does
- [call](11_call.md) — the other statement that takes a path, and where a relative one is resolved from
- [Settings](../Pob/06_Settings.md) — `stop_hook`, the same shell command run at the end of a macro
- [How it runs](13_How%20it%20runs.md) — the delay between statements, and what Stop does
- [Logs](../Pob/05_Logs.md) — where the line about a command, and what it printed, is written
