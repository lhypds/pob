// Package config reads and maintains <root>/settings.json, instruction.txt
// and macro.txt. It mirrors the behavior of the old Swift SettingsService:
// defaults are created on first run and missing keys are backfilled into an
// existing settings file. Values are re-read from disk on every access so
// edits take effect without restarting.
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
}

var defaults = map[string]any{
	"openai_api_key":      "",
	"base_url":            "https://api.openai.com/v1",
	"model":               "gpt-4o",
	"mcp_server_port":     8032,
	"start_mcp":           true,
	"max_steps":           12,
	"max_resumes":         5,
	"max_steplogs":        10,
	"macro_default_delay": 1000,
	"editor":              "system",
	"terminal":            "system",
	"stop_hook":           "",
}

func New(root string) *Config {
	c := &Config{Root: root}
	c.ensureFiles()
	return c
}

func (c *Config) settingsFile() string    { return filepath.Join(c.Root, "settings.json") }
func (c *Config) instructionFile() string { return filepath.Join(c.Root, "instruction.txt") }
func (c *Config) macroFile() string       { return filepath.Join(c.Root, "macro.txt") }
func (c *Config) LogsDir() string         { return filepath.Join(c.Root, "logs") }

func (c *Config) ensureFiles() {
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
	_ = os.MkdirAll(c.LogsDir(), 0o755)
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
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.settingsFile(), data, 0o644)
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

func (c *Config) MCPPort() int           { return c.intVal("mcp_server_port", 8032, 1) }
func (c *Config) MaxSteps() int          { return c.intVal("max_steps", 12, 1) }
func (c *Config) MaxStepLogs() int       { return c.intVal("max_steplogs", 10, 1) }
func (c *Config) MaxResumes() int        { return c.intVal("max_resumes", 5, 1) }
func (c *Config) MacroDefaultDelay() int { return c.intVal("macro_default_delay", 1000, 0) }

func (c *Config) StartMCP() bool {
	if v, ok := c.readSettings()["start_mcp"].(bool); ok {
		return v
	}
	return true
}

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

func (c *Config) AppendToMacro(line string) {
	f, err := os.OpenFile(c.macroFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}
