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
		// And both are handed to psl as the same thing.
		if Prepare(pair[0]) != Prepare(pair[1]) {
			t.Errorf("Prepare(%q) = %q, Prepare(%q) = %q — want them identical",
				pair[0], Prepare(pair[0]), pair[1], Prepare(pair[1]))
		}
	}
}

// psl reads the markers more strictly than Pob does: it wants them spaced out.
// Everything it would fill, Pob sees too — the other way round is what Prepare
// exists to fix.
func TestTheCompilerIsStricter(t *testing.T) {
	for _, tt := range []struct {
		text     string
		pob      bool
		compiler bool
	}{
		{`typeText(:: what to say ::)`, true, true},
		{`typeText(::what to say::)`, true, false},
		{`move(:: the x offset::, 40)`, true, false},
		{`typeText("std::cout")`, false, false},
		{`click()`, false, false},
	} {
		if got := HasSlot(tt.text); got != tt.pob {
			t.Errorf("HasSlot(%q) = %v, want %v", tt.text, got, tt.pob)
		}
		if got := CompilerHasSlot(tt.text); got != tt.compiler {
			t.Errorf("CompilerHasSlot(%q) = %v, want %v", tt.text, got, tt.compiler)
		}
	}
}

// Prepare hands psl exactly one slot: the first, spaced out so it is filled,
// and the rest closed up so they are not.
func TestPrepare(t *testing.T) {
	tests := []struct{ text, want string }{
		{`click()`, `click()`},
		{`move(::the x offset::, 40)`, `move(:: the x offset ::, 40)`},
		{`move(:: the x offset ::, 40)`, `move(:: the x offset ::, 40)`},
		{`move(::a::, ::b::)`, `move(:: a ::, ::b::)`},
		{`move(:: a ::, :: b ::)`, `move(:: a ::, ::b::)`},
		{`typeText("Hi ::the name::")`, `typeText("Hi :: the name ::")`},
	}
	for _, tt := range tests {
		got := Prepare(tt.text)
		if got != tt.want {
			t.Errorf("Prepare(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// Whatever it is handed, Prepare leaves the compiler exactly one slot to fill
// and Neutralize leaves it none.
func TestPrepareAndNeutralizeAgainstTheCompiler(t *testing.T) {
	for _, text := range []string{
		`move(::a::, ::b::)`,
		`move(:: a ::, :: b ::)`,
		`if (::a dialog is open::) {`,
		`typeText("::one:: and :: two ::")`,
		`:: alone ::`,
		`click()`,
	} {
		prepared := Prepare(text)
		if HasSlot(text) {
			slot, found := findSlot(prepared, 0, true)
			if !found {
				t.Errorf("Prepare(%q) = %q, which psl would not fill at all", text, prepared)
				continue
			}
			if _, more := findSlot(prepared, slot.End, true); more {
				t.Errorf("Prepare(%q) = %q, which leaves psl more than one slot", text, prepared)
			}
		} else if CompilerHasSlot(prepared) {
			t.Errorf("Prepare(%q) = %q, which psl would fill though Pob sees no slot", text, prepared)
		}

		if got := Neutralize(text); CompilerHasSlot(got) {
			t.Errorf("Neutralize(%q) = %q, which psl would still fill", text, got)
		}
	}
}
