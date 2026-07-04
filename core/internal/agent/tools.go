package agent

// makeTools returns the OpenAI function-tool definitions for step execution,
// identical to the old Swift AgentService.makeTools().
func makeTools() []map[string]any {
	fn := func(name, description string, properties map[string]any, required []string) map[string]any {
		if properties == nil {
			properties = map[string]any{}
		}
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": description,
				"parameters": map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			},
		}
	}
	num := func(description string) map[string]any {
		return map[string]any{"type": "number", "description": description}
	}
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}

	return []map[string]any{
		fn("move",
			"Nudge the cursor by a relative pixel offset in screenshot space. All coordinates are screenshot pixels (origin = top-left, x increases right, y increases down). The app converts to real screen coordinates — you never deal with screen-level positions. You will receive a new screenshot showing the updated cursor position.",
			map[string]any{
				"dx": num("Horizontal offset in screenshot pixels. Positive = right, negative = left."),
				"dy": num("Vertical offset in screenshot pixels. Positive = down, negative = up."),
			}, []string{"dx", "dy"}),
		fn("click",
			"Left-click at the current cursor position. Executes immediately; you will receive a screenshot after the click.",
			nil, nil),
		fn("rightClick",
			"Right-click at the current cursor position. Executes immediately.",
			nil, nil),
		fn("doubleClick",
			"Double-click at the current cursor position. Executes immediately.",
			nil, nil),
		fn("drag",
			"Drag from the current cursor position by (dx, dy) screenshot pixels. The cursor ends at the new position and you receive an updated screenshot.",
			map[string]any{
				"dx": num("Horizontal drag offset in screenshot pixels. Positive = right."),
				"dy": num("Vertical drag offset in screenshot pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		fn("scroll",
			"Scroll at the current cursor position. dy > 0 = scroll down, dy < 0 = scroll up, dx > 0 = scroll right.",
			map[string]any{
				"dx": num("Horizontal scroll amount in pixels."),
				"dy": num("Vertical scroll amount in pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		fn("typeText",
			"Type text at the current keyboard focus.",
			map[string]any{
				"text": str("The text to type."),
			}, []string{"text"}),
		fn("keyPress",
			"Press a special key. Supported: return, tab, space, delete, escape, left, right, up, down, home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.",
			map[string]any{
				"key": str("Key name, e.g. \"return\", \"escape\", \"cmd+v\"."),
			}, []string{"key"}),
		fn("sleep",
			"Pause execution for a given number of milliseconds.",
			map[string]any{
				"milliseconds": num("Number of milliseconds to sleep."),
			}, []string{"milliseconds"}),
		fn("take_screenshot",
			"Capture a fresh screenshot of the current screen state without moving or clicking. The screenshot is saved to disk but is NOT sent to the AI — use this only when you need to record the screen state for logging purposes, not when you need to see the screen. Optionally crop to a region using (crop_x, crop_y, crop_width, crop_height) in screenshot pixels.",
			map[string]any{
				"crop_x":      num("Left edge of the crop region in screenshot pixels."),
				"crop_y":      num("Top edge of the crop region in screenshot pixels."),
				"crop_width":  num("Width of the crop region in screenshot pixels."),
				"crop_height": num("Height of the crop region in screenshot pixels."),
			}, nil),
	}
}
