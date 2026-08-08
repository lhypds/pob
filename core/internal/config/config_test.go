package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pob/core/internal/storage"
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

// What an instance is doing sits under <root>/<instance>/; how the machine is
// configured sits above it. Pointing INSTANCE at another id is what gives a
// machine a clean instruction and macro — not a machine to set up again.
func TestInstanceOwnsItsWorkAndTheRootHoldsTheSettings(t *testing.T) {
	root := t.TempDir()
	New(root, "pb-aaaa")

	for _, name := range []string{"instruction.txt", "macro.txt"} {
		if _, err := os.Stat(filepath.Join(root, "pb-aaaa", name)); err != nil {
			t.Errorf("%s is missing from the instance directory: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s was written at the root", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "settings.json")); err != nil {
		t.Errorf("settings.json is missing from the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "pb-aaaa", "settings.json")); err == nil {
		t.Error("settings.json was written into the instance directory")
	}
}

// A second instance is the same machine: it is configured already, rather than
// starting from the defaults with the API key to set again.
func TestASecondInstanceSharesTheSettings(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "settings.json"), map[string]any{"server_port": 9100})

	if port := New(root, "pb-bbbb").ServerPort(); port != 9100 {
		t.Errorf("ServerPort() = %d, want 9100 — the machine's settings", port)
	}
}

// POB_SERVER_PORT is what a shell or a .env sets to pick the port per launch.
func TestServerPortEnvOverride(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "settings.json"), map[string]any{"server_port": 9100})
	t.Setenv("POB_SERVER_PORT", "9200")

	if port := New(root, "pb-aaaa").ServerPort(); port != 9200 {
		t.Errorf("ServerPort() = %d, want 9200", port)
	}
}

// A settings file where it used to be kept becomes the machine's, so a machine
// that has been set up stays set up.
func TestInstanceSettingsMoveUpToTheRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"openai_api_key": "sk-test",
		"server_port":    9100,
	})

	cfg := New(root, "pb-aaaa")
	if cfg.APIKey() != "sk-test" || cfg.ServerPort() != 9100 {
		t.Errorf("APIKey() = %q, ServerPort() = %d, want the settings carried up", cfg.APIKey(), cfg.ServerPort())
	}
	if _, err := os.Stat(filepath.Join(root, "pb-aaaa", "settings.json")); err == nil {
		t.Error("the instance still has a settings.json, want it moved rather than copied")
	}
}

// Two instances configured differently cannot both become the machine's. The
// first one up wins and the other is left where it is, rather than folded in
// or thrown away.
func TestOnlyTheFirstInstanceSettingsMoveUp(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "settings.json"), map[string]any{"server_port": 9100})
	write(t, filepath.Join(root, "pb-bbbb", "settings.json"), map[string]any{"server_port": 9200})

	if port := New(root, "pb-bbbb").ServerPort(); port != 9100 {
		t.Errorf("ServerPort() = %d, want 9100 — the machine's settings are not overruled", port)
	}
	left := readSettingsFile(filepath.Join(root, "pb-bbbb", "settings.json"))
	if left["server_port"] != float64(9200) {
		t.Errorf("the leftover settings = %v, want them left alone for their owner", left)
	}
}

// The window frame moves to instance.json, where the shells now read and write
// it: a window opens where it was left rather than jumping back to the middle
// of the screen on the run that moves it.
func TestWindowFrameMovesToInstanceJSON(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"server_port":   9100,
		"window_x":      68,
		"window_y":      874,
		"window_width":  695,
		"window_height": 580,
	})

	cfg := New(root, "pb-aaaa")

	instance := storage.ReadInstance(root, "pb-aaaa")
	if instance.ID != "pb-aaaa" {
		t.Fatalf("ReadInstance = %+v, want the instance", instance)
	}
	moved := readSettingsFile(filepath.Join(root, "pb-aaaa", "instance.json"))
	for key, want := range map[string]float64{
		"window_x": 68, "window_y": 874, "window_width": 695, "window_height": 580,
	} {
		if moved[key] != want {
			t.Errorf("instance.json[%s] = %v, want %v", key, moved[key], want)
		}
	}

	// Out of the file people open to edit, and the rest of it untouched.
	settings := readSettingsFile(cfg.settingsFile())
	for _, key := range windowFrameKeys {
		if _, stale := settings[key]; stale {
			t.Errorf("%s is still in settings.json", key)
		}
	}
	if settings["server_port"] != float64(9100) {
		t.Errorf("server_port = %v, want 9100 — the rest of the settings are untouched", settings["server_port"])
	}
}

// A frame already recorded by the shell is what the window was actually left
// at; the copy still sitting in settings.json is the older one and does not
// overrule it.
func TestRecordedWindowFrameWinsOverTheSettingsCopy(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"window_x": 68, "window_y": 874, "window_width": 695, "window_height": 580,
	})
	write(t, filepath.Join(root, "pb-aaaa", "instance.json"), map[string]any{
		"id": "pb-aaaa", "window_x": 10, "window_y": 20, "window_width": 300, "window_height": 400,
	})

	New(root, "pb-aaaa")

	instance := readSettingsFile(filepath.Join(root, "pb-aaaa", "instance.json"))
	if instance["window_x"] != float64(10) || instance["window_width"] != float64(300) {
		t.Errorf("instance.json = %v, want the frame the shell recorded", instance)
	}
	if _, stale := readSettingsFile(filepath.Join(root, "settings.json"))["window_x"]; stale {
		t.Error("window_x is still in settings.json, want it dropped either way")
	}
}
