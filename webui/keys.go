package webui

import "strings"

// Both clients — the page in index.html and the Pob Keyboard app — name keys
// the way the pico-hid HTTP API does: the *physical* key, in the style of the
// USB HID usage tables, so the target machine applies its own layout. Pob's
// own keyPress vocabulary spells the same keys in lower case and, in a few
// places, differently. This is the map between the two.
//
// A chord arrives as "+"-joined parts with the key last: "CTRL+SHIFT+c".

var picoModifiers = map[string]string{
	"CTRL":  "ctrl",
	"ALT":   "alt",
	"SHIFT": "shift",
	"GUI":   "gui",
}

// modOrder is the order modifiers are written back out in. Pob's shells don't
// care, but one fixed order keeps what shows up in the log readable.
var modOrder = []string{"ctrl", "alt", "shift", "gui"}

var picoKeyNames = map[string]string{
	"ENTER":         "return",
	"TAB":           "tab",
	"SPACE":         "space",
	"BACKSPACE":     "backspace",
	"DELETE":        "forwarddelete", // the HID DELETE is forward delete
	"ESCAPE":        "escape",
	"INSERT":        "insert",
	"HOME":          "home",
	"END":           "end",
	"PAGE_UP":       "pageup",
	"PAGE_DOWN":     "pagedown",
	"CAPS_LOCK":     "capslock",
	"PRINT_SCREEN":  "printscreen",
	"SCROLL_LOCK":   "scrolllock",
	"PAUSE":         "pause",
	"APPLICATION":   "menu",
	"UP":            "up",
	"DOWN":          "down",
	"LEFT":          "left",
	"RIGHT":         "right",
	"MINUS":         "minus",
	"EQUALS":        "equals",
	"LEFT_BRACKET":  "leftbracket",
	"RIGHT_BRACKET": "rightbracket",
	"BACKSLASH":     "backslash",
	"SEMICOLON":     "semicolon",
	"QUOTE":         "quote",
	"GRAVE_ACCENT":  "grave",
	"COMMA":         "comma",
	"PERIOD":        "period",
	"FORWARD_SLASH": "slash",
}

func init() {
	for _, n := range []string{
		"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
		"F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20", "F21", "F22", "F23", "F24",
	} {
		picoKeyNames[n] = strings.ToLower(n)
	}
}

// asciiKeys is the US layout, which is the one the API's single-character key
// names assume: the character a key produces, mapped back to the key itself
// and whether shift was on it. The keypad's "*" comes through here, and so
// does anything a caller sends as a literal character rather than a name.
var asciiKeys = map[rune]struct {
	key   string
	shift bool
}{
	'`': {"grave", false}, '~': {"grave", true},
	'-': {"minus", false}, '_': {"minus", true},
	'=': {"equals", false}, '+': {"equals", true},
	'[': {"leftbracket", false}, '{': {"leftbracket", true},
	']': {"rightbracket", false}, '}': {"rightbracket", true},
	'\\': {"backslash", false}, '|': {"backslash", true},
	';': {"semicolon", false}, ':': {"semicolon", true},
	'\'': {"quote", false}, '"': {"quote", true},
	',': {"comma", false}, '<': {"comma", true},
	'.': {"period", false}, '>': {"period", true},
	'/': {"slash", false}, '?': {"slash", true},
	' ': {"space", false},
	'!': {"1", true}, '@': {"2", true}, '#': {"3", true}, '$': {"4", true},
	'%': {"5", true}, '^': {"6", true}, '&': {"7", true}, '*': {"8", true},
	'(': {"9", true}, ')': {"0", true},
}

// pobKey turns one pico-hid chord into the key string Pob's keyPress takes,
// or reports that nothing here can press it.
func pobKey(chord string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(chord), "+")
	// A lone "+" is the key itself, not an empty chord with a separator: the
	// split leaves three empty strings, so put the character back.
	if strings.TrimSpace(chord) == "+" {
		parts = []string{"+"}
	}

	held := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		mod, ok := picoModifiers[strings.ToUpper(strings.TrimSpace(part))]
		if !ok {
			return "", false
		}
		held[mod] = true
	}

	key, shifted, ok := pobKeyName(parts[len(parts)-1])
	if !ok {
		return "", false
	}
	if shifted {
		held["shift"] = true
	}

	out := make([]string, 0, len(modOrder)+1)
	for _, mod := range modOrder {
		if held[mod] {
			out = append(out, mod)
		}
	}
	return strings.Join(append(out, key), "+"), true
}

// pobKeyName resolves the key half of a chord, reporting whether reaching it
// needs shift held — which is how a single shifted character such as "*" gets
// pressed.
func pobKeyName(name string) (key string, shift bool, ok bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, false
	}
	if runes := []rune(name); len(runes) == 1 {
		c := runes[0]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			return string(c), false, true
		case c >= 'A' && c <= 'Z':
			return strings.ToLower(string(c)), true, true
		}
		if entry, found := asciiKeys[c]; found {
			return entry.key, entry.shift, true
		}
		return "", false, false
	}
	if key, found := picoKeyNames[strings.ToUpper(name)]; found {
		return key, false, true
	}
	return "", false, false
}
