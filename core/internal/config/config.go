// Package config reads and maintains the machine's settings.json and an
// instance's macro.psl. It mirrors the behavior of the old Swift
// SettingsService: defaults are created on first run and missing keys are
// backfilled into an existing settings file. Values are re-read from disk on
// every access so edits take effect without restarting.
//
// settings.json sits at <root>, shared by every instance: the API key, the
// model and the port are how a machine works, not what one instance is doing
// with it, so moving <root>/INSTANCE to another id does not mean setting them
// again. What an instance owns is its own — macro.psl and its logs/ tree,
// under <root>/<instance>/ — and pointing INSTANCE at a new id gives a machine
// a clean set of those, which is what changing it is for.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pob/core/internal/mcpserver"
	"pob/core/internal/psl"
	"pob/core/internal/storage"
	"pob/server"
)

type Config struct {
	Root string
	// InstanceID names the <root>/<instance> directory this process works in.
	// An empty one is resolved the way the app resolves it, so the CLI reaches
	// the same files without a shell to tell it which they are.
	InstanceID string
}

var defaults = map[string]any{
	"psl":                 psl.DefaultBinary,
	"image_scale":         DefaultImageScale,
	"macro_default_delay": 1000,
	"editor":              "system",
	"terminal":            "system",
	"stop_hook":           "",
	"server":              true,
	"server_port":         server.DefaultPort,
	"webui_view_fps":      server.DefaultViewFPS,
	"mcp":                 true,
	"mcp_port":            mcpserver.DefaultPort,
	"mcp_host":            mcpserver.DefaultHost,
}

// legacyKeys are settings renamed since they were written, mapped old to new.
// A settings file from an older Pob is carried over on first read rather than
// left behind: a machine that had moved its port would otherwise go quietly
// back to the default one, and the address that was worth writing down would
// stop working.
var legacyKeys = map[string]string{
	"webui":      "server",
	"webui_port": "server_port",
}

func New(root, instanceID string) *Config {
	if instanceID == "" {
		instanceID = storage.ResolveInstanceID(root)
	}
	c := &Config{Root: root, InstanceID: instanceID}
	c.ensureFiles()
	return c
}

// InstanceDir is <root>/<instance>, everything this instance owns.
func (c *Config) InstanceDir() string { return filepath.Join(c.Root, c.InstanceID) }

// settingsFile is <root>/settings.json — the machine's settings, not one
// instance's. The API key, the model and the server port are how this machine
// works whichever instance it is running, so switching instances no longer
// means setting them again.
func (c *Config) settingsFile() string { return filepath.Join(c.Root, "settings.json") }

// legacySettingsFile is <root>/<instance>/settings.json, where settings used
// to be kept — one file per instance. See migrateSettingsToRoot.
func (c *Config) legacySettingsFile() string {
	return filepath.Join(c.InstanceDir(), "settings.json")
}

// macroFile is <root>/<instance>/macro.psl, the Prompt Script Language program
// Play and `pob macro` run.
func (c *Config) macroFile() string { return filepath.Join(c.InstanceDir(), "macro.psl") }

// legacyMacroFile is macro.txt, where the macro used to be kept before it had
// a language and an extension of its own. See migrateMacroToPSL.
func (c *Config) legacyMacroFile() string { return filepath.Join(c.InstanceDir(), "macro.txt") }

func (c *Config) ensureFiles() {
	// The instance directory must exist before any file below is written —
	// the CLI resolves to a not-yet-created ~/.pob. Making it makes the root.
	_ = os.MkdirAll(c.InstanceDir(), 0o755)

	// Before the settings move up: the frame belongs to this instance, and
	// only one instance's settings can become the machine's.
	c.migrateWindowFrame(c.legacySettingsFile())
	c.migrateSettingsToRoot()
	c.migrateMacroToPSL()

	if _, err := os.Stat(c.settingsFile()); os.IsNotExist(err) {
		c.writeSettings(defaults)
	} else {
		migrateLegacyKeys(c.settingsFile())
		c.migrateWindowFrame(c.settingsFile())
		settings := c.readSettings()
		changed := false
		for key, value := range defaults {
			if _, ok := settings[key]; !ok {
				settings[key] = value
				changed = true
			}
		}
		if changed {
			c.writeSettings(settings)
		}
	}
	if _, err := os.Stat(c.macroFile()); os.IsNotExist(err) {
		_ = os.WriteFile(c.macroFile(), []byte(""), 0o644)
	}
}

// migrateMacroToPSL carries an instance's macro over to macro.psl, the name it
// has now that the vocabulary it is written in is a language with a name of its
// own. A recording is work someone did with the app, so it is moved rather than
// left behind under a name nothing reads any more.
//
// A directory that already has a macro.psl keeps it — the old file is then a
// leftover from a Pob that ran before the move, and overwriting the current
// macro with it would lose whatever has been recorded since.
func (c *Config) migrateMacroToPSL() {
	if _, err := os.Stat(c.legacyMacroFile()); err != nil {
		return
	}
	if _, err := os.Stat(c.macroFile()); err == nil {
		return
	}
	_ = os.Rename(c.legacyMacroFile(), c.macroFile())
}

// migrateLegacyKeys rewrites settings that have been renamed, keeping the
// value that was set against the old name. A file that already uses the new
// name wins — the old key is then only a leftover, and is dropped.
func migrateLegacyKeys(path string) {
	settings := readSettingsFile(path)
	changed := false
	for old, current := range legacyKeys {
		value, ok := settings[old]
		if !ok {
			continue
		}
		delete(settings, old)
		if _, taken := settings[current]; !taken {
			settings[current] = value
		}
		changed = true
	}
	if changed {
		writeSettingsFile(path, settings)
	}
}

// windowFrameKeys are where the shell last had the window. They are not
// settings — nobody edits them and nothing about the app follows from them —
// so they live in instance.json with the rest of what an instance records
// about itself, and the shells read and write them there.
var windowFrameKeys = []string{"window_x", "window_y", "window_width", "window_height"}

// migrateWindowFrame carries the frame over from a settings file, where it
// used to be kept, so a window does not jump on the first run after the move.
// The keys are dropped from that file either way: once they are out of it
// nothing reads them from there, and leaving them behind would only leave
// something stale in a file people open to edit.
func (c *Config) migrateWindowFrame(path string) {
	settings := readSettingsFile(path)
	frame := map[string]any{}
	for _, key := range windowFrameKeys {
		if value, ok := settings[key]; ok {
			frame[key] = value
			delete(settings, key)
		}
	}
	if len(frame) == 0 {
		return
	}
	storage.FillInstance(c.Root, c.InstanceID, frame)
	writeSettingsFile(path, settings)
}

// migrateSettingsToRoot moves an instance's settings.json up to <root>, which
// is where the machine's settings now live.
//
// Only the first one moves. A machine that already has its settings keeps
// them, and the instance file left behind is a leftover rather than something
// to fold in — two instances configured differently cannot both win, and
// guessing which should would be worse than leaving the older one where it is
// for its owner to copy across.
func (c *Config) migrateSettingsToRoot() {
	if _, err := os.Stat(c.legacySettingsFile()); err != nil {
		return
	}
	if _, err := os.Stat(c.settingsFile()); err == nil {
		return
	}
	_ = os.Rename(c.legacySettingsFile(), c.settingsFile())
}

func (c *Config) readSettings() map[string]any { return readSettingsFile(c.settingsFile()) }

func readSettingsFile(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func (c *Config) writeSettings(settings map[string]any) {
	writeSettingsFile(c.settingsFile(), settings)
}

func writeSettingsFile(path string, settings map[string]any) {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// SettingsDict returns the full settings map (stored into session.json).
func (c *Config) SettingsDict() map[string]any { return c.readSettings() }

func (c *Config) str(key, fallback string) string {
	if v, ok := c.readSettings()[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// boolVal reads an on/off setting. Hand-edited files hold "true"/"false" as
// often as booleans, so both are read; anything unreadable falls back, which
// for the two servers means on.
func (c *Config) boolVal(key string, fallback bool) bool {
	switch v := c.readSettings()[key].(type) {
	case bool:
		return v
	case string:
		enabled, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		return enabled
	}
	return fallback
}

// portVal reads a port setting, with the named environment variable taking
// precedence — that is how a shell or a .env picks a port per launch, without
// editing the settings the machine keeps.
func (c *Config) portVal(key, env string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port < 65536 {
			return port
		}
	}
	return c.intVal(key, fallback, 1)
}

// floatVal reads a setting that is not a whole number, clamped to the range
// the thing reading it can actually use. Out of range is clamped rather than
// refused: a settings file is hand-edited, and a 60 meant as "as fast as it
// goes" should give the fastest rate there is, not silently fall back to the
// default one.
func (c *Config) floatVal(key string, fallback, minimum, maximum float64) float64 {
	var n float64
	switch v := c.readSettings()[key].(type) {
	case float64:
		n = v
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return fallback
		}
		n = parsed
	default:
		return fallback
	}
	return min(maximum, max(minimum, n))
}

func (c *Config) intVal(key string, fallback, minimum int) int {
	switch v := c.readSettings()[key].(type) {
	case float64:
		return max(minimum, int(v))
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return max(minimum, n)
		}
	}
	return fallback
}

func (c *Config) StopHook() string { v, _ := c.readSettings()["stop_hook"].(string); return v }

// PSLBinary is the psl compiler Pob runs to fill the :: … :: slots in a macro:
// a name to find on the PATH, or a path to the executable. Which models it uses
// and what keys they take are psl's own business, kept in its .pslrc — Pob
// holds no API key of its own.
func (c *Config) PSLBinary() string { return c.str("psl", psl.DefaultBinary) }

// DefaultImageScale halves the screenshot, and MinImageScale is as small as it
// can be asked to go — a tenth across is a hundredth of the pixels, past which
// nothing is legible anyway.
//
// A half rather than the whole picture because the whole one is not measurably
// better at the job, and a half is a third of the tokens: over 300 answers from
// one frontier vision model, asked for the centre of ten small controls in a
// 1736×1384 window, the median error was 1.0px at 1 and 0.0px at 0.5, and it
// read the half-size picture for 1018 input tokens instead of 2991.
//
// A half rather than less because of what breaks, which is not precision. Six of
// those controls were adjacent glyphs in one row, 47px apart. Down to 0.35 — 16px
// apart in the picture the model is shown — every answer named the right one, a
// few pixels off. At 0.3, 14px apart, 7 answers in 60 named the glyph beside it:
// a wrong click rather than a coarse one. Roughly, a scale has to keep 15px
// between the things a macro clicks, so 0.35 clears this window by nothing and a
// denser toolbar would not clear it at all.
//
// Going lower is also slower, which is the same fact from the other side: a
// model given an ambiguous picture spends longer on it. 4.3s a slot at 0.5, 7.5s
// at 0.35, 22.2s at 0.3. So 0.5 is the last scale free of both costs.
const (
	DefaultImageScale = 0.5
	MinImageScale     = 0.1
)

// ImageScale shrinks the screenshot a `:: … ::` slot is filled from, before it
// goes to the model: 1 is the picture as taken, 0.5 half as wide and half as
// tall — a quarter of the pixels, and roughly a quarter of the image tokens a
// vision model spends reading it.
//
// What it buys is the vision encoder, which has to run over every patch before
// the answer's first token is written — on one local 8B model, 15.1s of a slot
// at 1 and 6.1s of the same slot at DefaultImageScale. It buys nothing off the
// other half of a slot, the answer being written a token at a time, and that
// half can be the larger by far: a model that reasons first spends minutes on a
// slot that wants the word true, and no picture size touches it. So the token
// counts in the slot's log are what say whether this setting is the one to
// reach for.
//
// Raising it back to 1 is for a slot that asks about something smaller than the
// icons the default was measured on — a character in a line of text, a hairline
// border. What it costs is in DefaultImageScale.
//
// Pob undoes the shrinking on the answer, so a macro is written in screen
// pixels whatever this is set to. See rescaleFilled.
func (c *Config) ImageScale() float64 {
	return c.floatVal("image_scale", DefaultImageScale, MinImageScale, 1)
}

// ServerEnabled reports whether the Pob server should run. It is on by
// default; turning it off is how a machine stops accepting pointer and
// keyboard commands from the local network — which also takes the web UI down
// with it, since that page is served by the same server.
func (c *Config) ServerEnabled() bool { return c.boolVal("server", true) }

// ServerPort is the port this machine's Pob is reached through. It is the
// same on every machine unless someone changes it, so the address can be
// typed from memory. POB_SERVER_PORT overrides the setting, for a shell or a
// .env that wants to pick it per launch.
func (c *Config) ServerPort() int {
	return c.portVal("server_port", "POB_SERVER_PORT", server.DefaultPort)
}

// ViewFPS is how often the /view page refetches the picture. It is a machine
// setting rather than something the page offers, because the cost of a high
// rate lands on this machine — every frame is a screen capture here — and the
// machine is where it is known what that is worth: a laptop on battery and a
// desktop on a wired network do not want the same number, and neither of them
// wants it decided by whoever last opened the page on a phone.
func (c *Config) ViewFPS() float64 {
	return c.floatVal("webui_view_fps", server.DefaultViewFPS, server.MinViewFPS, server.MaxViewFPS)
}

// MCPEnabled reports whether the MCP server should run. Like the Pob server it
// is on by default and comes up with the instance, so a client that has pob
// registered finds it there without anything being started by hand; `false` is
// how a machine keeps the port closed.
func (c *Config) MCPEnabled() bool { return c.boolVal("mcp", true) }

// MCPPort is the port MCP clients reach this machine through. It is written
// into their config once and read from settings.json on every launch after
// that, so it has to be the same port every time — POB_MCP_PORT overrides it
// for a shell that wants to pick one per launch.
func (c *Config) MCPPort() int {
	return c.portVal("mcp_port", "POB_MCP_PORT", mcpserver.DefaultPort)
}

// MCPHost is the interface the MCP server binds. Every interface by default,
// so a client on another machine reaches it with nothing configured first —
// the same posture as the Pob server, which is open to the network and can
// type on this machine too. "127.0.0.1" closes it to loopback, for a machine
// on a network its owner would rather not trust. POB_MCP_HOST overrides the
// setting, the same way POB_MCP_PORT overrides the port.
func (c *Config) MCPHost() string {
	if v := strings.TrimSpace(os.Getenv("POB_MCP_HOST")); v != "" {
		return v
	}
	return c.str("mcp_host", mcpserver.DefaultHost)
}

func (c *Config) MacroDefaultDelay() int { return c.intVal("macro_default_delay", 1000, 0) }

func (c *Config) Macro() string {
	data, err := os.ReadFile(c.macroFile())
	if err != nil {
		return ""
	}
	return string(data)
}

func (c *Config) AppendToMacro(line string) {
	f, err := os.OpenFile(c.macroFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}
