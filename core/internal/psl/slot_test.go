package psl

import "testing"

// A macro is PSL all the way down, so the spaces are optional: `::x::` and
// `:: x ::` are the same slot. The identifier guards stay, since they are what
// keeps a `::` in text being typed from being read as a question.
func TestFindSlot(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		from        int
		instruction string
		found       bool
		span        string // what the marker covers, for the substitution to replace
	}{
		{"spaced out", `typeText(:: what to say ::)`, 0, "what to say", true, ":: what to say ::"},
		{"closed up", `typeText(::what to say::)`, 0, "what to say", true, "::what to say::"},
		{"spaced on one side only", `move(:: the x offset::, 40)`, 0, "the x offset", true, ":: the x offset::"},
		{"inside a string", `typeText("Hi ::the name::")`, 0, "the name", true, "::the name::"},
		{"one of two arguments", `move(::the x offset::, 40)`, 0, "the x offset", true, "::the x offset::"},
		{"an if condition", `if (::a save dialog is on screen::) {`, 0, "a save dialog is on screen", true, "::a save dialog is on screen::"},
		{"trimmed", `move(::   padded   ::, 40)`, 0, "padded", true, "::   padded   ::"},
		{"alone on the line", `:: alone ::`, 0, "alone", true, ":: alone ::"},

		{"no marker", `click()`, 0, "", false, ""},
		{"one marker only", `typeText("a :: b")`, 0, "", false, ""},
		{"empty marker is not a slot", `typeText(":: ::")`, 0, "", false, ""},
		{"resumed past the first", `move(::a::, ::b::)`, 8, "b", true, "::b::"},

		// The identifier guards, which is why relaxing the spaces is safe.
		{"glued to an identifier on the left", `typeText("std::cout")`, 0, "", false, ""},
		{"a scope operator in typed text", `typeText("see std::cout here")`, 0, "", false, ""},
		{"between two words", `typeText("a::b::c")`, 0, "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot, found := FindSlot(tt.text, tt.from)
			if found != tt.found {
				t.Fatalf("FindSlot(%q, %d) found = %v, want %v", tt.text, tt.from, found, tt.found)
			}
			if !found {
				return
			}
			if slot.Instruction != tt.instruction {
				t.Errorf("instruction = %q, want %q", slot.Instruction, tt.instruction)
			}
			if span := tt.text[slot.Start:slot.End]; span != tt.span {
				t.Errorf("marker spans %q, want %q", span, tt.span)
			}
		})
	}
}

// However it is written, the same statement means the same thing.
func TestBothFormsAreTheSameSlot(t *testing.T) {
	pairs := [][2]string{
		{`typeText(::what to say::)`, `typeText(:: what to say ::)`},
		{`move(::the x offset::, 40)`, `move(:: the x offset ::, 40)`},
		{`if (::a dialog is open::) {`, `if (:: a dialog is open ::) {`},
		{`typeText("Hi ::the name::")`, `typeText("Hi :: the name ::")`},
	}
	for _, pair := range pairs {
		tight, _ := FindSlot(pair[0], 0)
		spaced, _ := FindSlot(pair[1], 0)
		if tight.Instruction != spaced.Instruction {
			t.Errorf("%q asks %q but %q asks %q", pair[0], tight.Instruction, pair[1], spaced.Instruction)
		}
		if !HasSlot(pair[0]) || !HasSlot(pair[1]) {
			t.Errorf("%q / %q — one of them is not a slot", pair[0], pair[1])
		}
	}
}

// Pob and psl read the markers by the same rule. Which slot gets filled is
// decided by writing the others out of the file, so a slot Pob does not see is
// one it cannot write out — and psl would fill that one instead.
func TestPobAndTheCompilerReadTheSameMarkers(t *testing.T) {
	for _, tt := range []struct {
		text string
		slot bool
	}{
		{`typeText(:: what to say ::)`, true},
		{`typeText(::what to say::)`, true},
		{`move(:: the x offset::, 40)`, true},
		{`move(::…::, 40)`, true},
		{`typeText("std::cout")`, false},
		{`click()`, false},
	} {
		if got := HasSlot(tt.text); got != tt.slot {
			t.Errorf("HasSlot(%q) = %v, want %v", tt.text, got, tt.slot)
		}
		if got := CompilerHasSlot(tt.text); got != tt.slot {
			t.Errorf("CompilerHasSlot(%q) = %v, want %v", tt.text, got, tt.slot)
		}
	}
}

// Whatever it is handed, Neutralize leaves psl nothing to fill.
func TestNeutralizeAgainstTheCompiler(t *testing.T) {
	for _, text := range []string{
		`move(::a::, ::b::)`,
		`move(:: a ::, :: b ::)`,
		`if (::a dialog is open::) {`,
		`typeText("::one:: and :: two ::")`,
		`:: alone ::`,
		`click()`,
	} {
		if got := Neutralize(text); CompilerHasSlot(got) {
			t.Errorf("Neutralize(%q) = %q, which psl would still fill", text, got)
		}
	}
}

// A written-out line has to stay on its own line. A `::` with nothing to close
// it is still an opening once the lines are joined into a file, and the live
// slot below would close it — psl would answer the span between them.
func TestNeutralizedTextCannotReachTheLiveSlot(t *testing.T) {
	for _, text := range []string{
		`typeText("a :: b")`,
		`typeText(":: ::")`,
		`typeText("a ::: b")`,
		`move(::a::, ::b::)`,
		`typeText("std::cout")`,
		`click()`,
	} {
		neutralized := Neutralize(text)
		file := neutralized + "\nclick()\n" + `if (:: the live one ::) {`

		slot, found := FindCompilerSlot(file, 0)
		if !found {
			t.Errorf("Neutralize(%q) = %q — psl finds no slot in the file at all", text, neutralized)
			continue
		}
		if slot.Instruction != "the live one" {
			t.Errorf("Neutralize(%q) = %q — psl would fill %q, want the live slot",
				text, neutralized, slot.Instruction)
		}
	}
}

// What was asked stays readable once the slot is written out: the file psl
// sees is also the context the model reads the live slot in.
func TestNeutralizeKeepsTheInstruction(t *testing.T) {
	tests := []struct{ text, want string }{
		{`typeText(:: what to say ::)`, `typeText(<what to say>)`},
		{`typeText(::what to say::)`, `typeText(<what to say>)`},
		{`move(::a::, ::b::)`, `move(<a>, <b>)`},
		{`typeText("std::cout")`, `typeText("std::cout")`},
		{`click()`, `click()`},
	}
	for _, tt := range tests {
		if got := Neutralize(tt.text); got != tt.want {
			t.Errorf("Neutralize(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}
