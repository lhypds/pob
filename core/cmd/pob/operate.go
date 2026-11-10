// Operating the machine from the terminal: `pob view`, and the pointer and
// keyboard commands that act on what it showed.
//
// Each is one of the MCP server's tools, called through the control API's
// /tool endpoint, so the terminal works in the same grid an MCP client does:
// coordinates are pixels of the last picture `pob view` saved, measured from
// its top-left corner. That picture is shrunk to a size no model host resizes
// again, and the instance converts on the way back in — a position read off it
// is a position to type here, whatever the screen's own resolution is.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// operateCommands maps the commands that are one tool call each to the tool.
// view, drag and scroll are more than that and have functions of their own.
var operateCommands = map[string]string{
	"click":       "click",
	"rightclick":  "right_click",
	"doubleclick": "double_click",
}

// callTool runs one tool on the instance and answers with its text and, for a
// screenshot, the picture. Any failure ends the command: an action that did
// not land is the one thing these commands must not report as done.
func callTool(inst *Instance, name string, arguments map[string]any) (string, []byte) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	// Generous: a capture can take the shell a moment, and typeText a while.
	result, err := inst.post("/tool", map[string]any{"name": name, "arguments": arguments}, 60*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			fail("the running Pob is older than this pob command and has no tools to call — `pob relaunch` after updating the app")
		}
		fail("%v", err)
	}
	text, _ := result["text"].(string)
	var image []byte
	if encoded, _ := result["image"].(string); encoded != "" {
		if image, err = base64.StdEncoding.DecodeString(encoded); err != nil {
			fail("the picture came back unreadable: %v", err)
		}
	}
	return text, image
}

// numbers reads a command's coordinates. They are pixels of a picture, so
// whole numbers are what is expected, but a fraction is taken rather than
// refused — it is what dividing a zoomed position back down produces.
func numbers(args []string, command, shape string) []float64 {
	out := make([]float64, len(args))
	for i, arg := range args {
		n, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			fail("%q is not a number — `pob %s %s`", arg, command, shape)
		}
		out[i] = n
	}
	return out
}

// cmdView is `pob view`: the picture every other command here is aimed with.
//
//	pob view                     the whole of what Pob frames
//	pob view 0 40 500 300        that region, at the same scale
//	pob view 0 40 500 300 --zoom that region at the screen's own resolution
//	pob view --out shot.png      saved there rather than in the temp directory
func cmdView(inst *Instance, args []string) {
	arguments := map[string]any{}
	out := ""
	var region []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--zoom", arg == "-zoom":
			arguments["zoom"] = true
		case arg == "--cursor", arg == "-cursor":
			arguments["with_cursor"] = true
		case arg == "--out", arg == "-out", arg == "-o":
			if i+1 >= len(args) {
				fail("--out takes the file to save the picture to")
			}
			i++
			out = args[i]
		case strings.HasPrefix(arg, "--out="):
			out = strings.TrimPrefix(arg, "--out=")
		case strings.HasPrefix(arg, "-") && !isNumber(arg):
			fail("unknown option %q — `pob view [x y width height] [--zoom] [--cursor] [--out file]`", arg)
		default:
			region = append(region, arg)
		}
	}
	switch len(region) {
	case 0:
		if arguments["zoom"] == true {
			fail("--zoom takes the region to zoom into — `pob view x y width height --zoom`")
		}
	case 4:
		box := numbers(region, "view", "x y width height")
		arguments["crop_x"], arguments["crop_y"] = box[0], box[1]
		arguments["crop_width"], arguments["crop_height"] = box[2], box[3]
	default:
		fail("a region is four numbers — `pob view x y width height` — and %d were given", len(region))
	}

	text, image := callTool(inst, "take_screenshot", arguments)
	if image == nil {
		fail("no picture came back")
	}
	if out == "" {
		dir := filepath.Join(os.TempDir(), "pob")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("cannot create %s: %v", dir, err)
		}
		out = filepath.Join(dir, "view-"+time.Now().Format("20060102-150405.000")+".png")
	}
	if err := os.WriteFile(out, image, 0o644); err != nil {
		fail("cannot save the picture: %v", err)
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	fmt.Println(out)
	if text != "" {
		fmt.Println(text)
	}
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// cmdPoint is click, rightclick and doubleclick: at x y when given, and where
// the cursor already is otherwise.
func cmdPoint(inst *Instance, command string, args []string) {
	arguments := map[string]any{}
	switch len(args) {
	case 0:
	case 2:
		at := numbers(args, command, "[x y]")
		arguments["x"], arguments["y"] = at[0], at[1]
	default:
		fail("`pob %s` takes both x and y, or neither", command)
	}
	text, _ := callTool(inst, operateCommands[command], arguments)
	fmt.Println(text)
}

// cmdMove is `pob move x y`: the cursor put there, and nothing pressed.
func cmdMove(inst *Instance, args []string) {
	if len(args) != 2 {
		fail("`pob move x y` — a position in the last view")
	}
	at := numbers(args, "move", "x y")
	text, _ := callTool(inst, "move_to", map[string]any{"x": at[0], "y": at[1]})
	fmt.Println(text)
}

// cmdDrag is `pob drag x1 y1 x2 y2`: pressed at the first point and let go at
// the second.
func cmdDrag(inst *Instance, args []string) {
	if len(args) != 4 {
		fail("`pob drag x1 y1 x2 y2` — where the drag starts and where it ends")
	}
	at := numbers(args, "drag", "x1 y1 x2 y2")
	callTool(inst, "move_to", map[string]any{"x": at[0], "y": at[1]})
	text, _ := callTool(inst, "drag_to", map[string]any{"x": at[2], "y": at[3]})
	fmt.Println(text)
}

// cmdScroll is `pob scroll dx dy [x y]`: the distance to scroll, in the view's
// pixels — dy > 0 is down — at x y when given and under the cursor otherwise.
func cmdScroll(inst *Instance, args []string) {
	if len(args) != 2 && len(args) != 4 {
		fail("`pob scroll dx dy [x y]` — how far, dy > 0 being down, and optionally where")
	}
	n := numbers(args, "scroll", "dx dy [x y]")
	if len(n) == 4 {
		// A scroll lands on whatever is under the cursor, so where is a move.
		callTool(inst, "move_to", map[string]any{"x": n[2], "y": n[3]})
	}
	text, _ := callTool(inst, "scroll", map[string]any{"dx": n[0], "dy": n[1]})
	fmt.Println(text)
}

// cmdType is `pob type <text>`: typed at the keyboard focus. The words of the
// command line are joined with single spaces, so quoting is only needed where
// the spacing matters or the shell would read a character as its own.
func cmdType(inst *Instance, args []string) {
	if len(args) == 0 {
		fail("`pob type <text>` — the text to type at the keyboard focus")
	}
	text, _ := callTool(inst, "type_text", map[string]any{"text": strings.Join(args, " ")})
	fmt.Println(text)
}

// cmdKey is `pob key <chord>…`: each pressed in turn. It stops at the first
// that fails, since the ones after it were typed to follow it.
func cmdKey(inst *Instance, args []string) {
	if len(args) == 0 {
		fail("`pob key <key>…` — return, escape, cmd+v, ctrl+shift+t; see docs/Pob/04_Keys.md")
	}
	for _, key := range args {
		text, _ := callTool(inst, "key_press", map[string]any{"key": key})
		fmt.Println(text)
	}
}

// cmdCursor is `pob cursor`: where the virtual cursor is, in the view's pixels.
func cmdCursor(inst *Instance) {
	text, _ := callTool(inst, "get_cursor_position", nil)
	fmt.Println(text)
}
