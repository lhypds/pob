
AI slot
=======

An AI slot is an instruction written where a value would go, wrapped in `::` on both sides:

```
:: instruction ::
```

The spaces are optional. `::instruction::` and `:: instruction ::` are the same slot, and one end may
be closed up and the other not. What the markers may not do is touch a letter or a digit on the
outside: `typeText("std::cout")` types `std::cout`, and `typeText("a::b::c")` types `a::b::c`. That
is what tells a slot from a `::` in the text being typed.

(This is psl's own rule, read the same way on both sides. It has to be: psl fills the first slot in
the file it is handed and no other, so Pob picks which one by writing every other slot out of the
copy it hands over — and a slot Pob did not recognise is one it would leave in, for psl to answer
instead.)

The instruction between the markers is a question for the model rather than something Pob carries
out itself. It goes **anywhere in a statement** — a whole argument, part of one, the condition of an
`if` or of a `loop`:

```
move(:: the x offset to the Save button ::, 0)
typeText(:: what to reply to this message ::)
typeText("Hi :: the name at the top of the chat ::, thanks!")
if (:: a save dialog is on screen ::) {
    keyPress("return")
}
loop (:: another unread message in the list ::, 10) {
    typeText(:: a short reply to the message on screen ::)
}
```

When the replay reaches the statement, Pob takes a screenshot and runs the psl compiler over the
macro — the file itself, whole and unaltered, with that screenshot as the slot's image. psl asks a
model, writes the answer in where the markers were, and hands the file back; Pob reads the statement
out of it, parses it as PSL and executes it. Everything outside the markers is written down and
means exactly what it says.

Nothing is prepended to the file and no statement is rewritten on the way over. What does travel
beside it is a description of the vocabulary — the [calls](06_Calls.md), what their arguments mean,
and that an offset is measured from the cursor rather than from the corner of the screen — handed to
psl as its `--prompt`, which is the flag a compiler takes a briefing on an API for. It says what the
calls are and never what to write: what a value has to come back as is what the statement around it
already says, and how a slot is filled at all is psl's own business.

A psl older than that flag fills the macro all the same, from the statement and the screenshot and
nothing else. Pob asks the one on the machine what it takes before sending anything, and notes in
the log when a run goes without.

That is the *prompt* in Prompt Script Language, and the whole of what separates it from a scripting
language. A call is a macro doing what it was told. A slot is a macro asking about a screen nobody
could describe to it in advance, at the moment it is looking at that screen.

What comes back

Nothing states what kind of value is wanted — the model is shown the statement and works that out
from where the slot sits in it, which is why the whole macro goes with it. What it answers has to
leave a statement that reads as PSL:

| Written | What the slot has to come back as |
|---------|-----------------------------------|
| `move(:: … ::, 40)` | a bare number — `-120` |
| `move(:: … ::)` | both numbers, commas and all — `-120, 40` |
| `typeText(:: … ::)` | a quoted string — `"Hello"` |
| `typeText("Hi :: … ::")` | bare text, the quotes are already there — `Bob` |
| `sleep(:: … ::)` | a time, unit and all — `3s` |
| `if (:: … ::)` | `true` or `false` |
| `loop (:: … ::, 5)` | `true` or `false` |

How much of a statement a slot stands for is what is written around it, which is the second and third
rows above. A slot that is one argument of several is answered with that argument;
`move(:: the profile icon ::)` is written where the whole argument list goes and is answered with
the whole list. That is the shorter thing to write when both numbers are the same question — the
offset to a thing on screen is one question and not two, and asking it once means one psl run
instead of two, off one screenshot instead of two.

What a slot cannot do is take arguments away. `move(:: the x offset ::, 40, 60)` is three arguments
before anything is filled and at least three after, and `move` takes two — so that one is refused
before the run, while `move(:: the profile icon ::)` is not.

Coordinates come back as screenshot pixels, and `move` and `drag` are relative to where the cursor
is now — the arrow the model can see in the screenshot — so what it answers is an offset from there,
not a position on the screen.

They are screenshot pixels whatever `image_scale` is set to (see [Settings](../Pob/06_Settings.md)), and it
is below `1` by default. So the model is shown a smaller picture than the screen and answers in that
picture's pixels, and Pob grows the answer back before it goes into the macro: a `move` filled from a
third-size screenshot and one filled from a whole one write the same line. Only the part the model wrote is grown — a
number already in the statement was never in the model's coordinates — and only where the numbers
are distances across the picture: `move`, `drag` and `takeScreenshot`. `sleep` is a time and
`scroll` is a wheel delta, so neither is touched.

A statement that does not read as PSL once its slots are filled is logged with what it was filled to
and skipped. Nothing is retried: the macro goes on to the next statement. A psl run that fails
outright — no model configured, no answer — leaves the statement unfilled and therefore unrun, the
same way. These are the mistakes the check before the run cannot catch, since what a slot fills to
is not written down until it is filled — which is why the replay goes on reading forgivingly while
the check does not.

Writing one

Write an instruction a screenshot can settle — "a chat window is open", "the file list is empty",
"the x offset to the Save button". The model is given the macro, the statement and the picture and
nothing else: it has no memory of what the statements before it actually did, so an instruction that
turns on that is one it cannot answer.

Whitespace around the instruction is trimmed, so `::a::`, `:: a ::` and `::  a  ::` all ask the same
thing. A pair of markers with nothing between them asks nothing and is not a slot. What the model
answers is values and never more program — one of them or a list of them, whichever the slot was
written to stand for — and a `::` in the answer is text, not another slot to fill.

A slot can name the model it wants, which is psl's own syntax passed straight through:

```
typeText(:: gpt-5.6> a short reply to the message on screen ::)
```

Each statement's slots are filled left to right, one psl run each, and each one is asked with the
earlier answers already in place — so the second slot of `move(:: … ::, :: … ::)` is asked about a
statement that already reads `move(-120, :: … ::)`. That is how psl and Pob stay on the same slot at
all: psl fills the first slot in the file it is given, Pob replays the file top to bottom, and every
answer goes back into the file before the next run, so the first slot left is always the one the
replay is waiting on.

Two kinds of statement would break that step, and both are settled by writing their slots out of the
file as `<instruction>` — there to be read, not to be answered. One is a statement that will never
run: the body of an `if` whose condition did not hold, or a line Pob could not read in the first
place. The other is a statement whose own fill failed, which the replay is finished with either way.
Neither is ever asked about, and a slot left on one of them would be answered in place of the
statement below it, from a screenshot taken for something else.

A `loop` is the one thing that puts a slot back into the file, which is the same step run backwards
and holds for the same reason. What it puts back is its own header and the statements under it —
everything above them is filled or written out by the time a pass begins, and everything below is
still as it was written — so the first slot left in the file is again the one the replay is waiting
on.

A macro with no slot never runs psl at all, and needs nothing installed. A macro that has one needs
psl to be found — Pob checks over the whole macro, and over the files it `call`s, before the first
statement runs rather than partway through, and puts up **psl needed** instead of moving the cursor.
Every fill is kept under `logs/<session>/slots/<n>/` with the screenshot it was answered from and
what psl said while filling it (see [Logs](../Pob/05_Logs.md)); `pob --session <id>` lists them. The whole
macro as it ended up — every slot filled, in one file — is kept beside them as
`logs/<session>/macro.txt`.


See also
--------

- [Calls](06_Calls.md) — the vocabulary a fill is described with
- [loop blocks](08_loop%20blocks.md) — the slots a pass asks again
- [Settings](../Pob/06_Settings.md) — `image_scale`, and where the `psl` executable is
- [Logs](../Pob/05_Logs.md) — where each slot the AI filled is kept
