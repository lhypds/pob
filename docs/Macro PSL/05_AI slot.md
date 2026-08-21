
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
`if`, of a `loop` or of a `once`:

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

and **anywhere a statement goes**, written on a line of its own:

```
click(120, 300)
:: click my mom and type a message, but do not send it ::
sleep(2s)
```

Those are the two kinds, and which one a slot is is decided by what is written around it rather than
by what it says. A slot inside a statement is a *value slot*: the statement is what runs, and the
slot is the piece of it nobody could write down in advance. A slot that is the whole line is a
*statement slot*: there is no statement around it to be the piece of, so what it stands for is the
statements themselves. Statement slots are their own section below.

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
from where the slot sits in it, which is why the whole macro goes with it. What a value slot answers
has to leave a statement that reads as PSL:

| Written | What the slot has to come back as |
|---------|-----------------------------------|
| `move(:: … ::, 40)` | a bare number — `-120` |
| `move(:: … ::)` | both numbers, commas and all — `-120, 40` |
| `click(:: … ::)` | both numbers again, this time the position in the picture — `398, 915` |
| `typeText(:: … ::)` | a quoted string — `"Hello"` |
| `typeText("Hi :: … ::")` | bare text, the quotes are already there — `Bob` |
| `sleep(:: … ::)` | a time, unit and all — `3s` |
| `if (:: … ::)` | `true` or `false` |
| `} else if (:: … ::)` | `true` or `false` — it is the `if` it says it is |
| `loop (:: … ::, 5)` | `true` or `false` |
| `once (:: … ::)` | `true` or `false` — asked again at every change in the screen |

How much of a statement a slot stands for is what is written around it, which is the second and third
rows above. A slot that is one argument of several is answered with that argument;
`move(:: the profile icon ::)` is written where the whole argument list goes and is answered with
the whole list. That is the shorter thing to write when both numbers are the same question — the
offset to a thing on screen is one question and not two, and asking it once means one psl run
instead of two, off one screenshot instead of two.

What a slot cannot do is take arguments away. `move(:: the x offset ::, 40, 60)` is three arguments
before anything is filled and at least three after, and `move` takes two — so that one is refused
before the run, while `move(:: the profile icon ::)` is not.

Coordinates come back as screenshot pixels, and which kind is what the statement around the slot
asks for. `move` and `drag` are relative to where the cursor is now — the arrow the model can see in
the screenshot — so what it answers there is an offset from that arrow, not a position on the
screen. `moveTo`, `dragTo` and a `click`, `rightClick` or `doubleClick` written with a target are
the other way round: the answer is the position in the picture itself. That is the easier question
of the two to ask about a screenshot, so `click(:: the Save button ::)` is usually the better line
to write than `move(:: the offset to the Save button ::)` followed by a `click()`.

They are screenshot pixels whatever `image_scale` is set to (see [Settings](../Pob/06_Settings.md)), and it
is below `1` by default. So the model is shown a smaller picture than the screen and answers in that
picture's pixels, and Pob grows the answer back before it goes into the macro: a `move` filled from a
third-size screenshot and one filled from a whole one write the same line. The temporary copy of the
file sent to the model has its existing coordinates shrunk into the same pixel grid too, so every
image-measured number the model sees agrees with the picture. Those temporary values are discarded
when the answer comes back: the existing source stays byte-for-byte as written and only the new part
is grown. This applies where numbers are measured on the picture: `move`, `moveTo`, `drag`, `dragTo`,
the three clicks and `takeScreenshot`. A position on a third-size picture is a third of the position
on the screen, the same as a distance is, so both kinds grow back by the same factor. `sleep` is a
time and `scroll` is a wheel delta, so neither is touched.

A statement that does not read as PSL once its slots are filled is logged with what it was filled to
and skipped. Nothing is retried: the macro goes on to the next statement. A psl run that fails
outright — no model configured, no answer — leaves the statement unfilled and therefore unrun, the
same way. These are the mistakes the check before the run cannot catch, since what a slot fills to
is not written down until it is filled — which is why the replay goes on reading forgivingly while
the check does not.

Statement slots

A slot on a line of its own is answered with the statements that belong there — one of them or
several, blocks and all — and with nothing else:

```
click(120, 300)
:: click my mom and type a message, but do not send it ::
sleep(2s)
```

and what line 2 there comes back as is a block of them:

```
click(398, 915)
sleep(500ms)
typeText("hi mom")
```

Nothing else has to be written for it. There is no statement around the slot saying what a value
would have to be, because it is not a value that goes there, and a statement slot is the one place
in the language where what comes back is program rather than data.

Which is what an instruction written on a line of its own asks for: work carried out on the screen,
and the statements that carry it out. `:: calculate 360 x 360 ::` on a line of its own fills to the
clicks that work the calculator in the picture, key by key — not to `129600`, which is the answer to
the question rather than a statement, and a line the replay logs and skips. The value slot is where
an instruction is answered: `typeText(:: 360 x 360 ::)` types the product, because a `typeText`
argument is data and there is a statement around it saying so.

What comes back is replayed where the line stands, as a file of its own — the same thing
[`call`](11_call.md) does with the file it names, and for the same reason. Every statement in a macro
is found by its line number: that is how an answer goes back where it came from, how a loop puts its
statements back, and how Pob and psl stay on the same slot at all. Statements written into the macro
where the one line was would move every statement under them off the number the parse found it at.
So the block is a file with line numbers of its own, and the line that asked keeps the one line it
always had — `sleep(2s)` above is still on line 3 afterwards.

Everything that follows from that is what follows from a `call`:

- **It is named for where it came from.** `main.macro.psl` line 2 generates `main.macro-line2.psl`, which is
  what psl is told the file is called and what the log puts in front of the block's own line
  numbers — so `macro-line2.psl line 3` is the third statement of the block, not of the macro.
- **A slot inside the block is the block's own.** The model may leave one, and it is filled when the
  block's replay reaches it, off a screenshot taken then, against the block's file. A statement slot
  inside a generated block generates another block under it.
- **`stop()` inside it ends everything**, the macro included. See [stop](10_stop.md).
- **A relative path in a `call` inside it** is resolved against the directory of the file that asked,
  which is where the macro is — the only place the block could have meant.
- **Eight files deep is as far as it goes**, counted together with `call`: both are a file replayed
  inside another, and the bound is the same bound.

The check has nothing to say about a statement slot. What comes back is not written down until it is
filled, so it is read when it arrives, the way a called file is: a statement in the block that does
not read as PSL is logged and skipped, and the ones around it still run. What the check does catch is
a line that is neither kind of slot — two slots on one line, or a slot with a word beside it — since
a statement slot already fills to as many statements as the instruction asks for.

The whole block is kept under `logs/<session>/slots/<n>/` with the screenshot it was read off, and the
line each `slot.json` names is a line of `main.macro.psl`, which has not moved. A statement slot in a
loop's body generates a block on every pass, and each of those is one of these directories — so the
slots are where a generated block is read, one fill at a time, rather than folded back into a copy of
the macro that could only hold one of them.

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

Three kinds of statement would break that step, and all three are settled by writing their slots out
of the file as `<instruction>` — there to be read, not to be answered. One is a statement that will
never run: the body of an `if` whose condition did not hold, or a line Pob could not read in the
first place. Another is a statement whose own fill failed, which the replay is finished with either
way. The third is a statement slot that has been filled, since the block it filled to is a file of
its own from that moment and a slot the model wrote into it belongs to that file. None is ever asked
about here, and a slot left on one of them would be answered in place of the statement below it,
from a screenshot taken for something else.

A [`loop`](08_loop%20blocks.md) and a [`once`](09_once%20blocks.md) are the two things that put a
slot back into the file, which is the same step run backwards and holds for the same reason. What
goes back is the block's own header and the statements under it — everything above them is filled or
written out by the time a pass begins, and everything below is still as it was written — so the
first slot left in the file is again the one the replay is waiting on. A statement slot in there
goes back to the `:: … ::` it was written as, and generates a block of its own each time: the screen
a pass is about is the one the pass before it changed.

A macro with no slot never runs psl at all, and needs nothing installed. A macro that has one needs
psl to be found — Pob checks over the whole macro, and over the files it `call`s, before the first
statement runs rather than partway through, and puts up **psl needed** instead of moving the cursor.
Every fill is kept under `logs/<session>/slots/<n>/` with the screenshot it was answered from and
what psl said while filling it (see [Logs](../Pob/05_Logs.md)); `pob --session <id>` lists them. Beside
them is `main.macro.psl`, the macro as it was written — the fills are read against it rather than
written into a copy of it, since a slot in a `loop` has an answer per pass, a slot in a `once` one
per change it acted on, and a slot never reached has none.


See also
--------

- [Calls](06_Calls.md) — the vocabulary a fill is described with
- [call](11_call.md) — the other way a file is replayed inside another, and the bound they share
- [loop blocks](08_loop%20blocks.md) — the slots a pass asks again
- [once blocks](09_once%20blocks.md) — the slots a change asks again
- [Settings](../Pob/06_Settings.md) — `image_scale`, and where the `psl` executable is
- [Logs](../Pob/05_Logs.md) — where each slot the AI filled is kept
