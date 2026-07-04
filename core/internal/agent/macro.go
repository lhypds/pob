package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
)

// runMacro replays macro.txt line by line.
func (r *Runner) runMacro(ctx context.Context) {
	if _, err := r.br.ResetCursor(); err != nil {
		return
	}

	lines := strings.Split(r.cfg.Macro(), "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	applog.Logf("Executing macro (%d actions)", nonEmpty)

	// Initial capture establishes the screenshot→screen coordinate context on
	// the Swift side before any click lands.
	if _, err := r.br.CaptureScreenshot(true, nil); err != nil {
		applog.Log("Macro: failed to get screenshot context")
		return
	}

	sessionID := r.store.CreateSession()
	r.store.SaveMacro(sessionID)
	macroStart := time.Now()
	applog.Logf("[%s] Macro session started", sessionID)

	for _, line := range lines {
		if ctx.Err() != nil {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		name, args, ok := parseMacroLine(trimmed)
		if !ok {
			applog.Logf("Macro: skipping line: %s", trimmed)
			continue
		}

		r.runMacroAction(ctx, sessionID, name, args)

		if delayMs := r.cfg.MacroDefaultDelay(); delayMs > 0 {
			sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond)
		}
	}

	r.store.SaveSessionStartEndTimes(sessionID, macroStart, time.Now())
	applog.Logf("[%s] Macro session times saved", sessionID)
	applog.Log("Macro execution complete")
}

func (r *Runner) runMacroAction(ctx context.Context, sessionID, name string, args []string) {
	num := func(i int) (float64, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, err := strconv.ParseFloat(args[i], 64)
		return v, err == nil
	}

	switch name {
	case "move":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.MoveCursor(dx, dy); err == nil {
			applog.Logf("[%s] Macro move(%d, %d) -> (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "click":
		if pos, err := r.br.Click(); err == nil {
			applog.Logf("[%s] Macro click at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "rightClick":
		if pos, err := r.br.RightClick(); err == nil {
			applog.Logf("[%s] Macro rightClick at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "doubleClick":
		if pos, err := r.br.DoubleClick(); err == nil {
			applog.Logf("[%s] Macro doubleClick at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "drag":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Drag(dx, dy); err == nil {
			applog.Logf("[%s] Macro drag(%d, %d) -> (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "scroll":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Scroll(int(dx), int(dy)); err == nil {
			applog.Logf("[%s] Macro scroll(%d, %d) at (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "typeText":
		if len(args) == 0 {
			return
		}
		text := args[0]
		applog.Logf("[%s] Macro typeText(%q)", sessionID, truncate(text, 80))
		_ = r.br.TypeText(text)

	case "keyPress":
		if len(args) == 0 {
			return
		}
		applog.Logf("[%s] Macro keyPress(%q)", sessionID, args[0])
		_ = r.br.KeyPress(args[0])

	case "sleep":
		ms, ok := num(0)
		if !ok {
			return
		}
		applog.Logf("[%s] Macro sleep(%dms)", sessionID, int(ms))
		sleepCtx(ctx, time.Duration(ms)*time.Millisecond)

	case "take_screenshot":
		var crop *bridge.CropRect
		if len(args) >= 4 {
			x, okX := num(0)
			y, okY := num(1)
			w, okW := num(2)
			h, okH := num(3)
			if okX && okY && okW && okH {
				crop = &bridge.CropRect{X: x, Y: y, W: w, H: h}
			}
		}
		if crop != nil {
			applog.Logf("[%s] Macro take_screenshot(crop: %d, %d, %d, %d)", sessionID, int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
		} else {
			applog.Logf("[%s] Macro take_screenshot", sessionID)
		}
		r.br.FlashScreenshot()
		if shot, err := r.br.CaptureScreenshot(true, crop); err == nil {
			r.store.SaveScreenshot(shot, sessionID)
		}

	default:
		applog.Logf("[%s] Macro: unknown action: %s", sessionID, name)
	}
}

// parseMacroLine parses `name(arg1, arg2)` or `name("quoted string")`.
func parseMacroLine(line string) (string, []string, bool) {
	openParen := strings.Index(line, "(")
	if openParen < 0 || !strings.HasSuffix(line, ")") {
		return "", nil, false
	}
	name := strings.TrimSpace(line[:openParen])
	if name == "" {
		return "", nil, false
	}

	argsStr := strings.TrimSpace(line[openParen+1 : len(line)-1])
	if argsStr == "" {
		return name, []string{}, true
	}

	if strings.HasPrefix(argsStr, "\"") {
		var result strings.Builder
		runes := []rune(argsStr)
		i := 1
		for i < len(runes) {
			ch := runes[i]
			if ch == '\\' {
				if i+1 < len(runes) {
					result.WriteRune(runes[i+1])
					i += 2
				} else {
					i++
				}
			} else if ch == '"' {
				break
			} else {
				result.WriteRune(ch)
				i++
			}
		}
		return name, []string{result.String()}, true
	}

	parts := strings.Split(argsStr, ",")
	args := make([]string, len(parts))
	for i, p := range parts {
		args[i] = strings.TrimSpace(p)
	}
	return name, args, true
}
