package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// An instance is created named, in its own directory, and says which instance
// it is — the id is written down, not only used as the directory name.
func TestCreateInstanceRecordsIDAndName(t *testing.T) {
	root := t.TempDir()

	info, err := CreateInstance(root, "  Work laptop  ")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Work laptop" {
		t.Errorf("Name = %q, want the trimmed name", info.Name)
	}
	if _, err := os.Stat(filepath.Join(root, info.ID, "logs")); err != nil {
		t.Errorf("logs/ is missing from the new instance: %v", err)
	}

	read := ReadInstance(root, info.ID)
	if read.ID != info.ID || read.Name != "Work laptop" {
		t.Errorf("ReadInstance = %+v, want id %s named Work laptop", read, info.ID)
	}
}

// Two instances are two directories: creating one leaves the other alone, and
// both are listed.
func TestListInstances(t *testing.T) {
	root := t.TempDir()
	first, _ := CreateInstance(root, "First")
	second, _ := CreateInstance(root, "Second")

	instances := ListInstances(root)
	if len(instances) != 2 {
		t.Fatalf("ListInstances returned %d instances, want 2", len(instances))
	}
	seen := map[string]string{}
	for _, info := range instances {
		seen[info.ID] = info.Name
	}
	if seen[first.ID] != "First" || seen[second.ID] != "Second" {
		t.Errorf("listed %v, want both instances under their own names", seen)
	}
}

// The one that ran most recently is offered first, and one that has never run
// comes after it.
func TestListInstancesOrdersByLastRun(t *testing.T) {
	root := t.TempDir()
	older, _ := CreateInstance(root, "Older")
	newer, _ := CreateInstance(root, "Newer")
	CreateInstance(root, "Never run")

	writeJSON(filepath.Join(root, older.ID, "instance.json"), map[string]any{
		"id": older.ID, "name": "Older", "start_time": 1000,
	})
	writeJSON(filepath.Join(root, newer.ID, "instance.json"), map[string]any{
		"id": newer.ID, "name": "Newer", "start_time": 2000,
	})

	instances := ListInstances(root)
	if instances[0].ID != newer.ID || instances[1].ID != older.ID {
		t.Errorf("order = %s, %s, %s; want the most recently run first",
			instances[0].Label(), instances[1].Label(), instances[2].Label())
	}
	if instances[2].Name != "Never run" {
		t.Errorf("last = %q, want the instance that has never run", instances[2].Name)
	}
}

// Pointing INSTANCE at an instance is what makes it the current one, and it is
// the only thing consulted: an id that is already there is not adopted on its
// own.
func TestSetInstanceIDIsWhatResolveReads(t *testing.T) {
	root := t.TempDir()
	first, _ := CreateInstance(root, "First")
	second, _ := CreateInstance(root, "Second")

	if err := SetInstanceID(root, second.ID); err != nil {
		t.Fatal(err)
	}
	if id := ResolveInstanceID(root); id != second.ID {
		t.Errorf("ResolveInstanceID() = %s, want %s", id, second.ID)
	}

	if err := SetInstanceID(root, first.ID); err != nil {
		t.Fatal(err)
	}
	if id := ResolveInstanceID(root); id != first.ID {
		t.Errorf("ResolveInstanceID() = %s, want %s", id, first.ID)
	}

	// Removing the pointer starts a new instance rather than picking one of
	// the two already there.
	if err := os.Remove(filepath.Join(root, instancePointer)); err != nil {
		t.Fatal(err)
	}
	id := ResolveInstanceID(root)
	if id == first.ID || id == second.ID {
		t.Errorf("ResolveInstanceID() = %s, want a new instance", id)
	}
}

// Junk is not an instance id, and pointing at it is refused rather than
// written down for the app to walk into.
func TestSetInstanceIDRejectsJunk(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "laptop", "pb-../escape", `pb-\escape`} {
		if err := SetInstanceID(root, bad); err == nil {
			t.Errorf("SetInstanceID(%q) was accepted", bad)
		}
	}
}
