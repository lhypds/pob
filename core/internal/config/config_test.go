package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, settings map[string]any) {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A settings file written by an older Pob keeps its values under the names
// they have now — a machine that had moved its port stays where it was put.
func TestLegacyServerKeysAreCarriedOver(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"webui":      false,
		"webui_port": 9000,
	})

	cfg := New(root, "pb-aaaa")
	if port := cfg.ServerPort(); port != 9000 {
		t.Errorf("ServerPort() = %d, want 9000", port)
	}
	if cfg.ServerEnabled() {
		t.Error("ServerEnabled() = true, want false")
	}

	// The rename is written back, so the file the Settings menu opens holds
	// the name that is read.
	settings := readSettingsFile(cfg.settingsFile())
	if _, stale := settings["webui_port"]; stale {
		t.Error("webui_port is still in the settings file")
	}
	if settings["server_port"] != float64(9000) {
		t.Errorf("server_port = %v, want 9000", settings["server_port"])
	}
}

// Both names at once means the old one is a leftover: what is read today wins.
func TestCurrentKeyWinsOverLegacyKey(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"webui_port":  9000,
		"server_port": 9100,
	})

	if port := New(root, "pb-aaaa").ServerPort(); port != 9100 {
		t.Errorf("ServerPort() = %d, want 9100", port)
	}
}

// Everything an instance owns sits under <root>/<instance>/, and nothing it
// owns sits above it: pointing INSTANCE at another id is what gives a machine
// a clean set of files.
func TestInstanceOwnsItsFiles(t *testing.T) {
	root := t.TempDir()
	New(root, "pb-aaaa")

	for _, name := range []string{"settings.json", "instruction.txt", "macro.txt"} {
		if _, err := os.Stat(filepath.Join(root, "pb-aaaa", name)); err != nil {
			t.Errorf("%s is missing from the instance directory: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s was written at the root", name)
		}
	}
}

// A second instance id starts from the defaults rather than picking up what
// the instance beside it was configured with.
func TestNewInstanceStartsFromDefaults(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{"server_port": 9100})

	want, _ := defaults["server_port"].(int)
	if port := New(root, "pb-bbbb").ServerPort(); port != want {
		t.Errorf("ServerPort() = %d, want the default %d", port, want)
	}
}

// POB_SERVER_PORT is what a shell or a .env sets to pick the port per launch.
func TestServerPortEnvOverride(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{"server_port": 9100})
	t.Setenv("POB_SERVER_PORT", "9200")

	if port := New(root, "pb-aaaa").ServerPort(); port != 9200 {
		t.Errorf("ServerPort() = %d, want 9200", port)
	}
}
