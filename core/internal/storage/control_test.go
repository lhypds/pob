package storage

import (
	"path/filepath"
	"testing"
)

func testStorage(t *testing.T, root, id string) *Storage {
	t.Helper()
	return New(root, id, func() map[string]any { return nil }, func() string { return "" })
}

// The control port shares instance.json with the name and the times, and the
// two are written in either order — the app advertises its port before it
// records that it started — so neither write may drop what the other put there.
func TestControlAndInstanceTimesCoexist(t *testing.T) {
	root := t.TempDir()
	info, err := CreateInstance(root, "Work laptop")
	if err != nil {
		t.Fatal(err)
	}
	store := testStorage(t, root, info.ID)

	// The order pob-core uses: the port first, the start time after it.
	store.WriteControl(4711, 57259)
	store.WriteInstanceStart()

	entry := readJSONFile(filepath.Join(root, info.ID, "instance.json"))
	if intField(entry, "port") != 57259 || intField(entry, "pid") != 4711 {
		t.Errorf("instance.json = %v, want the pid and port kept through WriteInstanceStart", entry)
	}
	if name, _ := entry["name"].(string); name != "Work laptop" {
		t.Errorf("name = %q, want the name `pob new` gave it", name)
	}
	if intField(entry, "start_time") == 0 {
		t.Error("start_time is missing, want the recorded start")
	}
}

// A stopped instance stops advertising itself: the port goes, and what the
// directory is stays.
func TestClearControlKeepsTheRest(t *testing.T) {
	root := t.TempDir()
	info, _ := CreateInstance(root, "Work laptop")
	store := testStorage(t, root, info.ID)

	store.WriteControl(4711, 57259)
	store.WriteInstanceStart()
	store.WriteInstanceEnd()
	store.ClearControl()

	entry := readJSONFile(filepath.Join(root, info.ID, "instance.json"))
	if _, ok := entry["port"]; ok {
		t.Errorf("instance.json = %v, want no port once stopped", entry)
	}
	if _, ok := entry["pid"]; ok {
		t.Errorf("instance.json = %v, want no pid once stopped", entry)
	}
	if intField(entry, "start_time") == 0 || intField(entry, "end_time") == 0 {
		t.Errorf("instance.json = %v, want both times kept", entry)
	}

	read := ReadInstance(root, info.ID)
	if read.Name != "Work laptop" || read.StartTime == 0 {
		t.Errorf("ReadInstance = %+v, want the instance still named and timed", read)
	}
}
