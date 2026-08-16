package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// A session keeps the macro as it was written, slots and all: the instance's own
// copy is edited and re-recorded, and a replay is read against the lines it
// happened from. What the slots were filled with is under slots/, a fill at a
// time — there is no second copy of the macro with the answers in it, since a
// slot in a loop has one answer per pass and no one of them is the macro.
func TestSessionKeepsTheMacroAsItWasWritten(t *testing.T) {
	root := t.TempDir()
	info, err := CreateInstance(root, "Work laptop")
	if err != nil {
		t.Fatal(err)
	}
	written := "move(:: the x offset ::, 40)\nclick()\n"
	store := New(root, info.ID, func() map[string]any { return nil })

	sessionID := store.CreateSession()
	store.SaveMacro(sessionID, SessionMacroName, written)

	dir := filepath.Join(root, info.ID, "logs", sessionID)
	if text, err := os.ReadFile(filepath.Join(dir, SessionMacroName)); err != nil || string(text) != written {
		t.Errorf("%s = %q (%v), want the macro as it was written", SessionMacroName, text, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "macro.txt")); err == nil {
		t.Error("macro.txt is in the session — the compiled macro is not kept any more")
	}
}

// A session started on another file — `pob start --macropsl` — keeps its copy
// under that file's own name, which is the name its slots were logged against
// and the name the replay knew it by. Kept as main.macro.psl instead, a session
// directory would say it replayed the instance's entry point when it did not.
func TestSessionKeepsANamedMacroUnderItsOwnName(t *testing.T) {
	root := t.TempDir()
	info, err := CreateInstance(root, "Work laptop")
	if err != nil {
		t.Fatal(err)
	}
	written := "click()\n"
	store := New(root, info.ID, func() map[string]any { return nil })

	sessionID := store.CreateSession()
	store.SaveMacro(sessionID, "login.macro.psl", written)

	dir := filepath.Join(root, info.ID, "logs", sessionID)
	if text, err := os.ReadFile(filepath.Join(dir, "login.macro.psl")); err != nil || string(text) != written {
		t.Errorf("login.macro.psl = %q (%v), want the macro as it was written", text, err)
	}
	if _, err := os.Stat(filepath.Join(dir, SessionMacroName)); err == nil {
		t.Errorf("%s is in the session — it ran login.macro.psl", SessionMacroName)
	}
}
