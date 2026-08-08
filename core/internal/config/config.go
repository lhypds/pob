// Package config reads and maintains <root>/settings.json, instruction.txt
// and macro.txt. It mirrors the behavior of the old Swift SettingsService:
// defaults are created on first run and missing keys are backfilled into an
// existing settings file. Values are re-read from disk on every access so
// edits take effect without restarting.
//
// When an instance ID is given, the active settings file is
// <root>/logs/<instance>/settings.json, seeded from the root settings.json
// so every instance starts from the shared template but edits only its own
// copy. instruction.txt and macro.txt stay shared at the root.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pob/server"
)

type Config struct {
	Root string
	// InstanceID is the logs/<instance> directory this process belongs to;
	// empty means settings live at the root (legacy single-instance layout).
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
	c := &Config{Root: root, InstanceID: instanceID}
	c.ensureFiles()
	return c
}

func (c *Config) rootSettingsFile() string { return filepath.Join(c.Root, "settings.json") }

func (c *Config) settingsFile() string {
	if c.InstanceID != "" {
		return filepath.Join(c.LogsDir(), c.InstanceID, "settings.json")
	}
	return c.rootSettingsFile()
}

func (c *Config) instructionFile() string { return filepath.Join(c.Root, "instruction.txt") }
func (c *Config) macroFile() string       { return filepath.Join(c.Root, "macro.txt") }
func (c *Config) LogsDir() string         { return filepath.Join(c.Root, "logs") }

func (c *Config) ensureFiles() {
	// Root (and logs/) must exist before any file below is written — the CLI
	// resolves to a not-yet-created ~/.pob.
	_ = os.MkdirAll(c.LogsDir(), 0o755)
	if c.InstanceID != "" {
		_ = os.MkdirAll(filepath.Join(c.LogsDir(), c.InstanceID), 0o755)
		c.seedInstanceSettings()
	}
	// The root template is migrated too, not just this instance's copy: it is
	// the file the Settings menu opens, so a key left under its old name there
	// would be edited to no effect.
	migrateLegacyKeys(c.rootSettingsFile())
	if _, err := os.Stat(c.settingsFile()); os.IsNotExist(err) {
		c.writeSettings(defaults)
	} else {
		migrateLegacyKeys(c.settingsFile())
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

// seedInstanceSettings copies the root settings.json into the instance
// directory the first time this instance starts, so it inherits the shared
// template but subsequent edits stay local to the instance. A missing root
// template is created from the defaults first so later instances seed from
// the same file.
func (c *Config) seedInstanceSettings() {
	if _, err := os.Stat(c.rootSettingsFile()); os.IsNotExist(err) {
		writeSettingsFile(c.rootSettingsFile(), defaults)
	}
	if _, err := os.Stat(c.settingsFile()); err == nil {
		return
	}
	data, err := os.ReadFile(c.rootSettingsFile())
	if err != nil {
		return // no readable root template; defaults are written by ensureFiles
	}
	_ = os.WriteFile(c.settingsFile(), data, 0o644)
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

// WriteInstruction replaces instruction.txt (shared at the root) — used by
// the CLI's `pob run "<text>"`.
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
