// The arrow-key instance chooser for `pob launch`. It draws the list once and
// redraws it in place as the selection moves, which needs nothing more than a
// terminal in raw mode and two ANSI escapes. Where that is not available the
// caller asks for a number instead — see selectInstance.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"pob/core/internal/storage"
)

// Keys the chooser answers to.
const (
	keyUp = iota
	keyDown
	keyEnter
	keyCancel
	keyOther
)

// chooseFromList shows the instances with one of them selected and moves the
// selection with the arrow keys (or k/j, for the vi-handed). Enter picks the
// one under the cursor; q or Ctrl-C abandons the choice. A digit picks that
// row outright, which is quicker than arrowing to it.
//
// Returns the index chosen and whether the terminal could be driven this way
// at all; a false second return means nothing was drawn and the caller should
// fall back to its numbered prompt.
func chooseFromList(instances []storage.InstanceInfo, selected int) (int, bool) {
	restore, ok := rawMode(os.Stdin)
	if !ok {
		return 0, false
	}
	// Put the terminal back before anything else can print, whichever way this
	// function is left.
	defer restore()

	out := bufio.NewWriter(os.Stdout)
	in := bufio.NewReader(os.Stdin)

	fmt.Fprint(out, "Which instance? (↑/↓ to move, enter to start, q to cancel)\r\n")
	drawList(out, instances, selected, false)
	out.Flush()

	// Every draw after the first goes back to the top of the list and over the
	// rows already on screen; drawing without the cursor move would leave a
	// second copy of the list below the first.
	redraw := func(final bool) {
		fmt.Fprintf(out, "\033[%dA", len(instances))
		drawList(out, instances, selected, final)
		out.Flush()
	}

	for {
		switch key, digit := readKey(in); key {
		case keyUp:
			selected = (selected - 1 + len(instances)) % len(instances)
		case keyDown:
			selected = (selected + 1) % len(instances)
		case keyEnter:
			redraw(true)
			return selected, true
		case keyCancel:
			redraw(true)
			return -1, true
		default:
			if digit >= 1 && digit <= len(instances) {
				selected = digit - 1
				redraw(true)
				return selected, true
			}
			continue // a key the chooser has no use for; nothing to redraw
		}
		redraw(false)
	}
}

// drawList writes one line per instance, the selected one marked and bold.
// `final` drops the marker column's cursor, leaving the list on screen as a
// record of what was picked.
func drawList(out *bufio.Writer, instances []storage.InstanceInfo, selected int, final bool) {
	for i, info := range instances {
		marker := "  "
		if i == selected {
			marker = "❯ "
		}
		line := fmt.Sprintf("%s%-24s %-9s %s", marker, info.Label(), info.ID, lastRun(info))
		if i == selected && !final {
			line = "\033[1m" + line + "\033[0m"
		}
		// \r to the start and \033[K over whatever the last draw left there.
		fmt.Fprintf(out, "\r\033[K%s\r\n", line)
	}
}

// readKey reads one keypress, resolving the escape sequences the arrow keys
// arrive as. The second return is the digit pressed (0 for anything else), so
// a list can be answered by number without arrowing to the row.
func readKey(in *bufio.Reader) (int, int) {
	b, err := in.ReadByte()
	if err != nil {
		return keyCancel, 0
	}
	switch b {
	case '\r', '\n':
		return keyEnter, 0
	case 'q', 'Q', 3: // 3 is Ctrl-C, which raw mode delivers as a byte
		return keyCancel, 0
	case 'k':
		return keyUp, 0
	case 'j':
		return keyDown, 0
	case 27: // ESC — an arrow key is ESC [ A/B
		if next, err := in.ReadByte(); err != nil || next != '[' {
			return keyCancel, 0
		}
		switch final, err := in.ReadByte(); {
		case err != nil:
			return keyCancel, 0
		case final == 'A':
			return keyUp, 0
		case final == 'B':
			return keyDown, 0
		}
		return keyOther, 0
	}
	if b >= '1' && b <= '9' {
		return keyOther, int(b - '0')
	}
	return keyOther, 0
}

// indexOfInstance is where an id sits in the list, or 0 when it isn't there —
// something has to be under the cursor when the list is first drawn.
func indexOfInstance(instances []storage.InstanceInfo, id string) int {
	for i, info := range instances {
		if info.ID == id {
			return i
		}
	}
	return 0
}

// plainList prints the same list as numbered rows, for the terminals the
// chooser cannot drive.
func plainList(instances []storage.InstanceInfo, current string) {
	fmt.Println("Instances:")
	for i, info := range instances {
		marker := " "
		if info.ID == current {
			marker = "*"
		}
		fmt.Printf("  %s %d) %-24s %-9s %s\n", marker, i+1, info.Label(), info.ID, lastRun(info))
	}
}

// answerToIndex resolves what was typed at the numbered prompt: a row number,
// a name, or an id. ok is false when it matches none of them.
func answerToIndex(instances []storage.InstanceInfo, answer string, def int) (int, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return def, true
	}
	for i := range instances {
		if fmt.Sprint(i+1) == answer {
			return i, true
		}
	}
	if info, found := findInstance(instances, answer); found {
		return indexOfInstance(instances, info.ID), true
	}
	return 0, false
}
