// Package config reads and maintains the machine's settings.json and an
// instance's instruction.txt and macro.txt. It mirrors the behavior of the old
// Swift SettingsService: defaults are created on first run and missing keys are
// backfilled into an existing settings file. Values are re-read from disk on
// every access so edits take effect without restarting.
//
// settings.json sits at <root>, shared by every instance: the API key, the
// model and the port are how a machine works, not what one instance is doing
// with it, so moving <root>/INSTANCE to another id does not mean setting them
// again. What an instance owns is its own — instruction.txt, macro.txt and its
// logs/ tree, under <root>/<instance>/ — and pointing INSTANCE at a new id
// gives a machine a clean set of those, which is what changing it is for.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	"openai_api_key":      "",
	"base_url":            "https://api.openai.com/v1",
	"model":               "gpt-4o",
	"max_steps":           12,
	"max_resumes":         5,
	"max_steplogs":        10,
	"macro_default_delay": 1000,
	"editor":              "system",
	"terminal":            "system",
	"stop_hook":           "",
	"server":              true,
	"server_port":         server.DefaultPort,
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

func (c *Config) instructionFile() string { return filepath.Join(c.InstanceDir(), "instruction.txt") }
func (c *Config) macroFile() string       { return filepath.Join(c.InstanceDir(), "macro.txt") }

func (c *Config) ensureFiles() {
	// The instance directory must exist before any file below is written —
	// the CLI resolves to a not-yet-created ~/.pob. Making it makes the root.
	_ = os.MkdirAll(c.InstanceDir(), 0o755)

	// Before the settings move up: the frame belongs to this instance, and
	// only one instance's settings can become the machine's.
	c.migrateWindowFrame(c.legacySettingsFile())
	c.migrateSettingsToRoot()

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
	if _, err := os.Stat(c.instructionFile()); os.IsNotExist(err) {
		_ = os.WriteFile(c.instructionFile(), []byte("Describe what you see in this screenshot and identify any UI elements."), 0o644)
	}
	if _, err := os.Stat(c.macroFile()); os.IsNotExist(err) {
		_ = os.WriteFile(c.macroFile(), []byte(""), 0o644)
	}
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

func (c *Config) APIKey() string {
	v, _ := c.readSettings()["openai_api_key"].(string)
	return v
}

func (c *Config) BaseURL() string  { return c.str("base_url", "https://api.openai.com/v1") }
func (c *Config) Model() string    { return c.str("model", "gpt-4o") }
func (c *Config) StopHook() string { v, _ := c.readSettings()["stop_hook"].(string); return v }

// ServerEnabled reports whether the Pob server should run. It is on by
// default; turning it off is how a machine stops accepting pointer and
// keyboard commands from the local network — which also takes the web UI down
// with it, since that page is served by the same server.
func (c *Config) ServerEnabled() bool {
	switch v := c.readSettings()["server"].(type) {
	case bool:
		return v
	case string:
		// Hand-edited files hold "true"/"false" as often as booleans. Anything
		// unreadable falls back to on, which is the default either way.
		enabled, err := strconv.ParseBool(strings.TrimSpace(v))
		return err != nil || enabled
	}
	return true
}

// ServerPort is the port this machine's Pob is reached through. It is the
// same on every machine unless someone changes it, so the address can be
// typed from memory. POB_SERVER_PORT overrides the setting, for a shell or a
// .env that wants to pick it per launch.
func (c *Config) ServerPort() int {
	if env := strings.TrimSpace(os.Getenv("POB_SERVER_PORT")); env != "" {
		if port, err := strconv.Atoi(env); err == nil && port > 0 && port < 65536 {
			return port
		}
	}
	return c.intVal("server_port", server.DefaultPort, 1)
}

func (c *Config) MaxSteps() int          { return c.intVal("max_steps", 12, 1) }
func (c *Config) MaxStepLogs() int       { return c.intVal("max_steplogs", 10, 1) }
func (c *Config) MaxResumes() int        { return c.intVal("max_resumes", 5, 1) }
func (c *Config) MacroDefaultDelay() int { return c.intVal("macro_default_delay", 1000, 0) }

func (c *Config) Instruction() string {
	data, err := os.ReadFile(c.instructionFile())
	if err != nil {
		return "Describe what you see in this screenshot."
	}
	return string(data)
}

func (c *Config) Macro() string {
	data, err := os.ReadFile(c.macroFile())
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteInstruction replaces this instance's instruction.txt — used by the
// CLI's `pob run "<text>"`.
func (c *Config) WriteInstruction(text string) error {
	return os.WriteFile(c.instructionFile(), []byte(text), 0o644)
}

func (c *Config) AppendToMacro(line string) {
	f, err := os.OpenFile(c.macroFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}
