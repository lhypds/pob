package agent

import "testing"

// nodeSummary renders a parsed macro as one line per statement, indented by
// depth, so a test can state the shape it expects. A statement still holding a
// slot is shown as it was written, since that is all that is known of it until
// the replay fills it.
func nodeSummary(nodes []macroNode, indent string) []string {
	var out []string
	for _, n := range nodes {
		if n.isIf {
			out = append(out, indent+"if ("+n.condition+")")
			out = append(out, nodeSummary(n.body, indent+"  ")...)
			continue
		}
		if n.slots {
			out = append(out, indent+"unfilled: "+n.raw)
			continue
		}
		line := indent + n.action + "("
		for i, a := range n.args {
			if i > 0 {
				line += ", "
			}
			line += a
		}
		out = append(out, line+")")
	}
	return out
}

func checkParse(t *testing.T, macro string, want []string) {
	t.Helper()
	got := nodeSummary(parseMacro(macro), "")
	if len(got) != len(want) {
		t.Fatalf("parsed %d statements, want %d:\ngot  %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseMacroActions(t *testing.T) {
	checkParse(t, `move(398, 915)
click()

typeText("hello, world")
`, []string{
		"move(398, 915)",
		"click()",
		`typeText(hello, world)`,
	})
}

func TestParseMacroIfBlock(t *testing.T) {
	checkParse(t, `move(398, 915)
click()
if (::the window focus on a wechat user::) {
    move(128, 738)
    click()
}
drag(-775, -615)`, []string{
		"move(398, 915)",
		"click()",
		"if (::the window focus on a wechat user::)",
		"  move(128, 738)",
		"  click()",
		"drag(-775, -615)",
	})
}

// A slot goes anywhere in a statement, not only in an if. What the statement
// says is not known at parse time, so it is kept as written and read again once
// the replay has filled it.
func TestParseMacroSlotsInAnyStatement(t *testing.T) {
	checkParse(t, `move(::the x offset to the Save button::, 40)
typeText(::what to say::)
typeText("Hi ::the name on screen::")
click()`, []string{
		"unfilled: move(::the x offset to the Save button::, 40)",
		"unfilled: typeText(::what to say::)",
		`unfilled: typeText("Hi ::the name on screen::")`,
		"click()",
	})
}

// A condition written out rather than asked needs no model call, and reads as
// the value it names.
func TestParseMacroLiteralCondition(t *testing.T) {
	checkParse(t, `if (true) {
	click()
}
if (false) {
	move(1, 2)
}`, []string{
		"if (true)",
		"  click()",
		"if (false)",
		"  move(1, 2)",
	})
}

// The keyword is written lowercase, and read whatever its case: a block that
// went unrecognised would run its body unguarded.
func TestParseMacroIfKeywordCase(t *testing.T) {
	for _, keyword := range []string{"if", "IF", "If"} {
		checkParse(t, keyword+` (::a chat window is open::) {
	click()
}`, []string{
			"if (::a chat window is open::)",
			"  click()",
		})
	}
}

func TestParseMacroNestedIf(t *testing.T) {
	checkParse(t, `if (::a chat window is open::) {
	if (::the message list is empty::) {
		typeText("hi")
	}
	click()
}`, []string{
		"if (::a chat window is open::)",
		"  if (::the message list is empty::)",
		"    typeText(hi)",
		"  click()",
	})
}

// An if left open runs to the end of the macro rather than dropping the lines
// under it — they stay guarded by the condition either way.
func TestParseMacroUnclosedIf(t *testing.T) {
	checkParse(t, `if (::a chat window is open::) {
	click()`, []string{
		"if (::a chat window is open::)",
		"  click()",
	})
}

// A `}` with no if above it is dropped, and the statements around it stand.
func TestParseMacroStrayCloseBrace(t *testing.T) {
	checkParse(t, `click()
}
move(1, 2)`, []string{
		"click()",
		"move(1, 2)",
	})
}

// A malformed if header still opens a block, and the block is dropped: lines
// written to be guarded must not run unguarded.
func TestParseMacroMalformedIfDropsBlock(t *testing.T) {
	for _, header := range []string{
		"if (::the window is focused::)",  // no {
		"if (::the window is focused:: {", // no )
		"if ::the window is focused:: {",  // no parentheses
		"if (the window is focused) {",    // neither a slot nor true/false
		"if (::::) {",                     // nothing to ask
		"if () {",
		"if {",
		"if",
	} {
		checkParse(t, header+`
	click()
}
move(1, 2)`, []string{
			"move(1, 2)",
		})
	}
}

// A line that only starts with the letters of the keyword is an action line.
func TestParseMacroIfPrefixIsNotKeyword(t *testing.T) {
	checkParse(t, `iframe(1, 2)`, []string{"iframe(1, 2)"})
	checkParse(t, `IFrame(1, 2)`, []string{"IFrame(1, 2)"})
}

func TestParseIfHeader(t *testing.T) {
	tests := []struct {
		line      string
		condition string
		isIf      bool
	}{
		{"if (::the window focus on a wechat user::) {", "::the window focus on a wechat user::", true},
		{"if  (  ::  spaced  out  ::  )  {", "::  spaced  out  ::", true},
		{"if\t(::a tab after the keyword::) {", "::a tab after the keyword::", true},
		{"if(::no space at all::){", "::no space at all::", true},
		{"IF (::read whatever its case::) {", "::read whatever its case::", true},
		{"if (::a { brace in the condition::) {", "::a { brace in the condition::", true},
		{"if (true) {", "true", true},
		{"if (FALSE) {", "FALSE", true},
		{"if (::no closing brace::)", "", true},
		{"if ::no parentheses:: {", "", true},
		{"if (no slot) {", "", true},
		{"if (::half a slot::) {", "::half a slot::", true},
		{"if (::half marked) {", "", true},
		{"if (::::) {", "", true},
		{"if () {", "", true},
		{"if {", "", true},
		{"if", "", true},
		{"click()", "", false},
		{"iframe(1, 2)", "", false},
		{"ifs(1, 2)", "", false},
	}
	for _, tt := range tests {
		condition, isIf := parseIfHeader(tt.line)
		if isIf != tt.isIf || condition != tt.condition {
			t.Errorf("parseIfHeader(%q) = (%q, %v), want (%q, %v)", tt.line, condition, isIf, tt.condition, tt.isIf)
		}
	}
}

func TestFindSlot(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		from   int
		prompt string
		found  bool
		span   string // what the marker covers, for the substitution to replace
	}{
		{"a whole argument", `typeText(::what to say::)`, 0, "what to say", true, "::what to say::"},
		{"inside a string", `typeText("Hi ::the name::")`, 0, "the name", true, "::the name::"},
		{"one of two arguments", `move(::the x offset::, 40)`, 0, "the x offset", true, "::the x offset::"},
		{"trimmed", `move(::  padded  ::, 40)`, 0, "padded", true, "::  padded  ::"},
		{"an if condition", `::a save dialog is on screen::`, 0, "a save dialog is on screen", true, "::a save dialog is on screen::"},
		{"no marker", `click()`, 0, "", false, ""},
		{"one marker only", `typeText("a::b")`, 0, "", false, ""},
		{"empty marker is not a slot", `typeText("::::")`, 0, "", false, ""},
		// Markers pair off left to right: the empty pair asks nothing and both
		// of its markers are used up, so the pair after it is the first slot.
		{"empty marker passed over", `typeText("::::x::y::")`, 0, "y", true, "::y::"},
		{"resumed past the first", `move(::a::, ::b::)`, 11, "b", true, "::b::"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot, found := findSlot(tt.text, tt.from)
			if found != tt.found {
				t.Fatalf("findSlot(%q, %d) found = %v, want %v", tt.text, tt.from, found, tt.found)
			}
			if !found {
				return
			}
			if slot.prompt != tt.prompt {
				t.Errorf("prompt = %q, want %q", slot.prompt, tt.prompt)
			}
			if span := tt.text[slot.start:slot.end]; span != tt.span {
				t.Errorf("marker spans %q, want %q", span, tt.span)
			}
		})
	}
}

// Every slot in a statement is filled, left to right, and the answers are
// substituted where the markers were.
func TestFillSlotsWith(t *testing.T) {
	answers := map[string]string{
		"the x offset": "-120",
		"the y offset": "40",
		"what to say":  `"Hello"`,
		"the name":     "Bob",
		"a dialog":     "true",
	}
	ask := func(_, prompt string) (string, bool) {
		value, ok := answers[prompt]
		return value, ok
	}

	tests := []struct {
		text string
		want string
	}{
		{`click()`, `click()`},
		{`move(::the x offset::, 40)`, `move(-120, 40)`},
		{`move(::the x offset::, ::the y offset::)`, `move(-120, 40)`},
		{`typeText(::what to say::)`, `typeText("Hello")`},
		{`typeText("Hi ::the name::")`, `typeText("Hi Bob")`},
		{`::a dialog::`, `true`},
	}
	for _, tt := range tests {
		got, ok := fillSlotsWith(tt.text, ask)
		if !ok {
			t.Errorf("fillSlotsWith(%q) gave up", tt.text)
			continue
		}
		if got != tt.want {
			t.Errorf("fillSlotsWith(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// A slot the AI could not answer stops the statement rather than leaving the
// marker in it to be run as text.
func TestFillSlotsWithUnanswered(t *testing.T) {
	ask := func(_, _ string) (string, bool) { return "", false }
	if got, ok := fillSlotsWith(`move(::the x offset::, 40)`, ask); ok {
		t.Errorf("fillSlotsWith = (%q, true), want it to give up", got)
	}
}

// The statement handed to each ask is the one as it stands, so a later slot is
// asked about with the earlier answers already in place.
func TestFillSlotsWithSeesEarlierAnswers(t *testing.T) {
	var seen []string
	ask := func(statement, prompt string) (string, bool) {
		seen = append(seen, statement)
		if prompt == "the x offset" {
			return "-120", true
		}
		return "40", true
	}
	if _, ok := fillSlotsWith(`move(::the x offset::, ::the y offset::)`, ask); !ok {
		t.Fatal("fillSlotsWith gave up")
	}
	want := []string{
		`move(::the x offset::, ::the y offset::)`,
		`move(-120, ::the y offset::)`,
	}
	if len(seen) != len(want) {
		t.Fatalf("asked %d times, want %d: %q", len(seen), len(want), seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("ask %d saw %q, want %q", i, seen[i], want[i])
		}
	}
}

// An answer that happens to hold `::` is a value, not more program: scanning
// resumes past it rather than reading it as another slot.
func TestFillSlotsWithAnswerHoldingMarkers(t *testing.T) {
	calls := 0
	ask := func(_, _ string) (string, bool) {
		calls++
		return `"a ::b:: c"`, true
	}
	got, ok := fillSlotsWith(`typeText(::what to say::)`, ask)
	if !ok {
		t.Fatal("fillSlotsWith gave up")
	}
	if calls != 1 {
		t.Errorf("asked %d times, want 1 — the answer was read as a slot", calls)
	}
	if want := `typeText("a ::b:: c")`; got != want {
		t.Errorf("fillSlotsWith = %q, want %q", got, want)
	}
}

// What the AI answers is substituted and the statement read again, so the shape
// of the answer is what decides what the statement becomes.
func TestFilledStatementsParse(t *testing.T) {
	tests := []struct {
		filled string
		name   string
		args   []string
	}{
		{`move(-120, 40)`, "move", []string{"-120", "40"}},
		{`typeText("Hello")`, "typeText", []string{"Hello"}},
		{`typeText("Hi Bob")`, "typeText", []string{"Hi Bob"}},
		{`keyPress("cmd+v")`, "keyPress", []string{"cmd+v"}},
	}
	for _, tt := range tests {
		name, args, ok := parseMacroLine(tt.filled)
		if !ok {
			t.Errorf("parseMacroLine(%q) did not read as a statement", tt.filled)
			continue
		}
		if name != tt.name {
			t.Errorf("parseMacroLine(%q) name = %q, want %q", tt.filled, name, tt.name)
		}
		if len(args) != len(tt.args) {
			t.Errorf("parseMacroLine(%q) args = %q, want %q", tt.filled, args, tt.args)
			continue
		}
		for i := range args {
			if args[i] != tt.args[i] {
				t.Errorf("parseMacroLine(%q) arg %d = %q, want %q", tt.filled, i, args[i], tt.args[i])
			}
		}
	}
}

// A macro with no slot never calls the model, so it needs nothing configured —
// and one with a slot anywhere in it, at any depth, does.
func TestHasMacroSlot(t *testing.T) {
	tests := []struct {
		name  string
		macro string
		want  bool
	}{
		{"no slot", "move(1, 2)\nclick()", false},
		{"a literal condition is not a slot", "if (true) {\n\tclick()\n}", false},
		{"an if condition", "click()\nif (::a dialog is open::) {\n\tclick()\n}", true},
		{"an argument", "move(::the x offset::, 40)", true},
		{"inside a string", `typeText("Hi ::the name::")`, true},
		{"nested in a block", "if (true) {\n\tif (::a dialog is open::) {\n\t\tclick()\n\t}\n}", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMacroSlot(parseMacro(tt.macro)); got != tt.want {
				t.Errorf("hasMacroSlot = %v, want %v", got, tt.want)
			}
		})
	}
}

// A condition holds only when it says so. Anything Pob cannot read as one of
// the two words is not a verdict, and the block it guards stays unexecuted —
// running statements a broken condition was written to guard is the one outcome
// nobody asked for.
func TestConditionHolds(t *testing.T) {
	tests := []struct {
		expr  string
		holds bool
		read  bool
	}{
		{"true", true, true},
		{"false", false, true},
		{"TRUE", true, true},
		{"False", false, true},
		{"  true  ", true, true},
		{`"true"`, true, true},
		{`"false"`, false, true},
		{"yes", false, false},
		{"1", false, false},
		{"", false, false},
		{"the dialog is open", false, false},
		{"true-ish", false, false},
	}
	for _, tt := range tests {
		holds, read := conditionHolds(tt.expr)
		if holds != tt.holds || read != tt.read {
			t.Errorf("conditionHolds(%q) = (%v, %v), want (%v, %v)", tt.expr, holds, read, tt.holds, tt.read)
		}
	}
}

func TestCountMacroNodes(t *testing.T) {
	nodes := parseMacro(`move(1, 2)
if (::something is true::) {
	click()
	if (::something else is true::) {
		click()
	}
}`)
	if got := countMacroNodes(nodes); got != 5 {
		t.Errorf("countMacroNodes = %d, want 5", got)
	}
}
