package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pob/core/internal/mcpserver"
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
// machine a clean macro — not a machine to set up again.
func TestInstanceOwnsItsWorkAndTheRootHoldsTheSettings(t *testing.T) {
	root := t.TempDir()
	New(root, "pb-aaaa")

	if _, err := os.Stat(filepath.Join(root, "pb-aaaa", "macro.psl")); err != nil {
		t.Errorf("macro.psl is missing from the instance directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "macro.psl")); err == nil {
		t.Error("macro.psl was written at the root")
	}
	if _, err := os.Stat(filepath.Join(root, "settings.json")); err != nil {
		t.Errorf("settings.json is missing from the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "pb-aaaa", "settings.json")); err == nil {
		t.Error("settings.json was written into the instance directory")
	}
}

// A macro recorded before macro.txt was renamed is work someone did with the
// app, so it comes over to macro.psl rather than being left behind under a name
// nothing reads any more.
func TestALegacyMacroIsCarriedOverToPSL(t *testing.T) {
	root := t.TempDir()
	instanceDir := filepath.Join(root, "pb-aaaa")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "macro.txt"), []byte("click()\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if macro := New(root, "pb-aaaa").Macro(); macro != "click()\n" {
		t.Errorf("Macro() = %q, want the lines that were in macro.txt", macro)
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "macro.txt")); err == nil {
		t.Error("macro.txt is still there — it should have been moved, not copied")
	}
}

// An instance that has a macro.psl already keeps it: the macro.txt beside it is
// then a leftover from a Pob that ran before the rename, and letting it win
// would lose whatever has been recorded since.
func TestALegacyMacroDoesNotOverwriteAnExistingPSL(t *testing.T) {
	root := t.TempDir()
	instanceDir := filepath.Join(root, "pb-aaaa")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "macro.txt"), []byte("click()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "macro.psl"), []byte("doubleClick()\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if macro := New(root, "pb-aaaa").Macro(); macro != "doubleClick()\n" {
		t.Errorf("Macro() = %q, want the macro.psl that was already there", macro)
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

// The view page's rate is the one setting that is not a whole number, and it
// is hand-edited: out of range is clamped to what the page can actually run at
// rather than thrown away, and a file that has never seen the key gets the
// default.
func TestViewFPSIsClampedToWhatThePageCanRun(t *testing.T) {
	for _, c := range []struct {
		name string
		set  any
		want float64
	}{
		{"unset", nil, 5},
		{"a fraction", 0.5, 0.5},
		{"written as text", "2.5", 2.5},
		{"past the ceiling", 60, 30},
		{"below the floor", 0, 0.1},
		{"not a number", "soon", 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			settings := map[string]any{}
			if c.set != nil {
				settings["webui_view_fps"] = c.set
			}
			write(t, filepath.Join(root, "settings.json"), settings)

			if got := New(root, "pb-aaaa").ViewFPS(); got != c.want {
				t.Errorf("ViewFPS() = %v, want %v", got, c.want)
			}
		})
	}
}

// The MCP server starts with the instance on the port a client's config names,
// so both settings have to be there for a file that has never seen them —
// and, like the Pob server, on by default.
func TestMCPDefaultsToOnAtItsOwnPort(t *testing.T) {
	root := t.TempDir()
	cfg := New(root, "pb-aaaa")

	if !cfg.MCPEnabled() {
		t.Error("MCPEnabled() = false, want true — it starts with the instance")
	}
	if port := cfg.MCPPort(); port != mcpserver.DefaultPort {
		t.Errorf("MCPPort() = %d, want %d", port, mcpserver.DefaultPort)
	}

	// Backfilled into the file too, so the Settings menu shows what is running.
	settings := readSettingsFile(cfg.settingsFile())
	if settings["mcp"] != true {
		t.Errorf("mcp = %v in settings.json, want true", settings["mcp"])
	}
	if settings["mcp_port"] != float64(mcpserver.DefaultPort) {
		t.Errorf("mcp_port = %v in settings.json, want %d", settings["mcp_port"], mcpserver.DefaultPort)
	}
}

// Turning it off is how a machine keeps the port closed, and a hand-edited
// file holds "false" as often as false.
func TestMCPCanBeTurnedOffAndMoved(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "settings.json"), map[string]any{
		"mcp":      "false",
		"mcp_port": 9032,
	})
	cfg := New(root, "pb-aaaa")

	if cfg.MCPEnabled() {
		t.Error(`MCPEnabled() = true with "mcp": "false", want false`)
	}
	if port := cfg.MCPPort(); port != 9032 {
		t.Errorf("MCPPort() = %d, want 9032", port)
	}

	t.Setenv("POB_MCP_PORT", "9033")
	if port := cfg.MCPPort(); port != 9033 {
		t.Errorf("MCPPort() = %d with POB_MCP_PORT set, want 9033", port)
	}
}

// A settings file where it used to be kept becomes the machine's, so a machine
// that has been set up stays set up.
func TestInstanceSettingsMoveUpToTheRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pb-aaaa", "settings.json"), map[string]any{
		"psl":         "/opt/psl",
		"server_port": 9100,
	})

	cfg := New(root, "pb-aaaa")
	if cfg.PSLBinary() != "/opt/psl" || cfg.ServerPort() != 9100 {
		t.Errorf("PSLBinary() = %q, ServerPort() = %d, want the settings carried up", cfg.PSLBinary(), cfg.ServerPort())
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

// image_scale is about a third of the picture unless someone asks for more of
// it, and a hand-edited file asking for more than the whole picture, or for none
// of it, is clamped rather than refused — a 0 would send the model nothing to
// read.
func TestImageScaleIsAThirdByDefaultAndClamped(t *testing.T) {
	// Pinned to the number rather than read off the constant: the default is a
	// measured choice with no margin in it — see DefaultImageScale — so moving it
	// is a decision to make deliberately, with the measurement redone, not a
	// constant to nudge.
	if DefaultImageScale != 0.35 {
		t.Errorf("DefaultImageScale = %v, want 0.35", DefaultImageScale)
	}
	root := t.TempDir()
	if got := New(root, "pb-aaaa").ImageScale(); got != DefaultImageScale {
		t.Errorf("ImageScale() = %v on a fresh machine, want %v", got, DefaultImageScale)
	}

	for _, c := range []struct{ set, want any }{
		{0.5, 0.5},
		{"0.25", 0.25},
		{2, 1.0},
		{0, MinImageScale},
		{-1, MinImageScale},
		{"half", DefaultImageScale},
	} {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "settings.json"), map[string]any{"image_scale": c.set})
		if got := New(dir, "pb-aaaa").ImageScale(); got != c.want {
			t.Errorf("ImageScale() = %v with image_scale %v, want %v", got, c.set, c.want)
		}
	}
}
