
call
====

`call` replays another PSL file where it stands, and comes back to the statement under it:

```
call("../sign-in.psl")
move(398, 915)
click()
call("../sign-out.psl")
```

The argument is a path, and a relative one is relative to the directory of the file the `call` is
written in — not to wherever Pob was started from. The macro lives in `~/.pob/<instance>/`, so
`call("../sign-in.psl")` is `~/.pob/sign-in.psl`, beside the instance directories and shared by all
of them. A path beginning with `~/` is under the home directory, and an absolute path is itself.

The called file is ordinary PSL: the same statements, the same blocks, the same `:: … ::` slots, and
its own `call`s, resolved against its own directory. It is read at the moment the call is reached
rather than once at the start, so a `call` inside a `loop` replays the file as it is written on
every pass — and editing a called file between two runs takes effect on the second, the way editing
the macro does.

What it is for is the piece of a macro that is the same in five macros. Signing in, closing whatever
dialog this application opens on startup, the six statements that get from the home screen to the
place the work happens: recorded once, kept in one file, and called from each of the macros that
needs it.

A file that calls itself, or a ring of files that comes back round to one already running, is a
replay with no end in it. Eight files deep is as far as `call` goes, which is the bound on the other
shape of the same mistake — a chain where every file is a new one. The check walks the calls before
the run and reports both, along with a path naming a file that is not there; the replay refuses
them again if it somehow meets one, since a path that was itself a slot is only known at the call.

The depth is counted over the blocks a [statement slot](05_AI%20slot.md) generated as well, since
those are files replayed inside another by the same machinery — a `call` inside a generated block is
one file deeper than the file that asked for the block, and a relative path in it is resolved against
that file's directory. A generated block has no file behind it, so nothing can `call` one and nothing
can reach one round a ring; what a `call` inside one *can* reach is the file it was generated in,
and that is refused as the file calling itself that it is.

Each file is its own program as far as psl is concerned. It is the file handed over on a fill, and
the line numbers a slot comes back to are its own — so the log names the file in front of the line
once more than one is in play, and `logs/<session>/slots/<n>/slot.json` records which file each fill
was a line of. The slot directories are numbered straight through the session however many files
filled them.

The check for psl follows `call` as well: a macro with no slot of its own that calls a file with one
still needs psl, and Pob reads the called files before the cursor moves rather than finding out
partway down. A `call` whose path is itself a slot cannot be read ahead — but a macro with a slot in
it needs psl anyway, which is the same answer.

`logs/<session>/main.macro.psl` and `macro.txt` are the macro, as they have always been. A called file is
not copied into the session; what the run made of it is in the log and in the slots it filled.


See also
--------

- [When something is wrong](11_When%20something%20is%20wrong.md) — what the check says about a ring, a depth and a missing file
- [stop](09_stop.md) — ending the calling file as well as the called one
- [Logs](../Pob/05_Logs.md) — which file each fill was a line of
