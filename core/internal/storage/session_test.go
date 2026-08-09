package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// A session keeps both macros: the one that was written, and the one that ran.
// Reading a replay back means reading them against each other, so neither may
// be written over the other.
func TestSessionKeepsBothTheWrittenAndTheCompiledMacro(t *testing.T) {
	root := t.TempDir()
	info, err := CreateInstance(root, "Work laptop")
	if err != nil {
		t.Fatal(err)
	}
	written := "move(:: the x offset ::, 40)\nclick()\n"
	store := New(root, info.ID, func() map[string]any { return nil }, func() string { return written })

	sessionID := store.CreateSession()
	store.SaveMacro(sessionID)
	store.SaveCompiledMacro(sessionID, "move(-120, 40)\nclick()\n")

	dir := filepath.Join(root, info.ID, "logs", sessionID)
	if text, err := os.ReadFile(filepath.Join(dir, "macro.psl")); err != nil || string(text) != written {
		t.Errorf("macro.psl = %q (%v), want the macro as it was written", text, err)
	}
	if text, err := os.ReadFile(filepath.Join(dir, "macro.txt")); err != nil || string(text) != "move(-120, 40)\nclick()\n" {
		t.Errorf("macro.txt = %q (%v), want the macro with its slot filled", text, err)
	}
}
