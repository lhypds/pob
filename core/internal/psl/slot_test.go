package psl

import "testing"

// The delimiters are psl's, not Pob's: a statement Pob thinks holds a slot and
// psl does not is one the replay would wait on forever, so these cases are the
// contract between the two.
func TestFindSlot(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		from        int
		instruction string
		found       bool
		span        string // what the marker covers, for the substitution to replace
	}{
		{"a whole argument", `typeText(:: what to say ::)`, 0, "what to say", true, ":: what to say ::"},
		{"inside a string", `typeText("Hi :: the name ::")`, 0, "the name", true, ":: the name ::"},
		{"one of two arguments", `move(:: the x offset ::, 40)`, 0, "the x offset", true, ":: the x offset ::"},
		{"an if condition", `if (:: a save dialog is on screen ::) {`, 0, "a save dialog is on screen", true, ":: a save dialog is on screen ::"},
		{"trimmed", `move(::   padded   ::, 40)`, 0, "padded", true, "::   padded   ::"},
		{"no marker", `click()`, 0, "", false, ""},
		{"one marker only", `typeText("a :: b")`, 0, "", false, ""},

		// psl's rules, which are what keep another language's own syntax out of
		// it — and, in a macro, what tells a slot from text being typed.
		{"closed up is not a slot", `typeText(::what to say::)`, 0, "", false, ""},
		{"glued to an identifier on the left", `typeText("std:: cout ::")`, 0, "", false, ""},
		{"no space after the opener", `move(::the x offset ::, 40)`, 0, "", false, ""},
		{"no space before the closer", `move(:: the x offset::, 40)`, 0, "", false, ""},

		{"empty marker is not a slot", `typeText(":: ::")`, 0, "", false, ""},
		{"resumed past the first", `move(:: a ::, :: b ::)`, 13, "b", true, ":: b ::"},
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

func TestHasSlot(t *testing.T) {
	for _, tt := range []struct {
		text string
		want bool
	}{
		{`click()`, false},
		{`move(:: the x offset ::, 40)`, true},
		{`typeText("Hi :: the name ::")`, true},
		{`typeText(::closed up::)`, false},
		{`if (true) {`, false},
	} {
		if got := HasSlot(tt.text); got != tt.want {
			t.Errorf("HasSlot(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// Closing a slot up is how Pob hands psl a macro with one live slot in it: the
// rest have to come back inert, or psl fills whichever comes first in the file
// rather than the one the replay is waiting on.
func TestNeutralize(t *testing.T) {
	tests := []struct{ text, want string }{
		{`click()`, `click()`},
		{`move(:: the x offset ::, 40)`, `move(::the x offset::, 40)`},
		{`move(:: a ::, :: b ::)`, `move(::a::, ::b::)`},
		{`typeText("Hi :: the name ::")`, `typeText("Hi ::the name::")`},
		{`typeText(::already closed up::)`, `typeText(::already closed up::)`},
	}
	for _, tt := range tests {
		got := Neutralize(tt.text)
		if got != tt.want {
			t.Errorf("Neutralize(%q) = %q, want %q", tt.text, got, tt.want)
		}
		if HasSlot(got) {
			t.Errorf("Neutralize(%q) = %q, which psl would still fill", tt.text, got)
		}
	}
}

// Whatever it is given comes back with nothing left for psl to find, however
// the markers were arranged.
func TestNeutralizeLeavesNothingLive(t *testing.T) {
	for _, text := range []string{
		`move(:: a ::, :: b ::)`,
		`if (:: a dialog is open ::) {`,
		`typeText(":: one :: and :: two ::")`,
		`:: alone ::`,
	} {
		if got := Neutralize(text); HasSlot(got) {
			t.Errorf("Neutralize(%q) = %q, which psl would still fill", text, got)
		}
	}
}
