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
	if _, err := os.Stat(c.settingsFile()); os.IsNotExist(err) {
		c.writeSettings(defaults)
	} else {
		json := c.readSettings()
		changed := false
		for key, value := range defaults {
			if _, ok := json[key]; !ok {
				json[key] = value
				changed = true
			}
		}
		if changed {
			c.writeSettings(json)
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

func (c *Config) readSettings() map[string]any {
	data, err := os.ReadFile(c.settingsFile())
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
