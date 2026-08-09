package agent

import "testing"

// nodeSummary renders a parsed macro as one line per statement, indented by
// depth, so a test can state the shape it expects.
func nodeSummary(nodes []macroNode, indent string) []string {
	var out []string
	for _, n := range nodes {
		if n.isIf() {
			out = append(out, indent+"if (::"+n.condition+"::)")
			out = append(out, nodeSummary(n.body, indent+"  ")...)
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
		"if (the window is focused) {",    // no ::…:: slot
		"if (::::) {",                     // nothing to judge
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
		{"if (::the window focus on a wechat user::) {", "the window focus on a wechat user", true},
		{"if  (  ::  spaced  out  ::  )  {", "spaced  out", true},
		{"if\t(::a tab after the keyword::) {", "a tab after the keyword", true},
		{"if(::no space at all::){", "no space at all", true},
		{"IF (::read whatever its case::) {", "read whatever its case", true},
		{"if (::a { brace in the condition::) {", "a { brace in the condition", true},
		{"if (::no closing brace::)", "", true},
		{"if ::no parentheses:: {", "", true},
		{"if (no slot) {", "", true},
		{"if (::half a slot::) {", "half a slot", true},
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
