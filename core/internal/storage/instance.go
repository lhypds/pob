package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// InstanceInfo is one instance directory: who it is, the name it was given,
// and when it last ran. Read from <root>/<id>/instance.json, which the CLI
// lists instances by and the core keeps the times in.
type InstanceInfo struct {
	ID        string
	Name      string
	Dir       string
	StartTime int64
	EndTime   int64
}

// Label is the name to show a person, falling back to the id for instances
// made before names existed — every instance has something to be called.
func (i InstanceInfo) Label() string {
	if strings.TrimSpace(i.Name) != "" {
		return i.Name
	}
	return i.ID
}

// CreateInstance reserves a new instance directory under root and names it.
// The id is written into instance.json beside the name, so the directory says
// which instance it is rather than only being named after it.
func CreateInstance(root, name string) (InstanceInfo, error) {
	id := newInstanceID(root)
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		return InstanceInfo{}, err
	}
	info := InstanceInfo{ID: id, Name: strings.TrimSpace(name), Dir: dir}
	writeJSON(filepath.Join(dir, "instance.json"), map[string]any{
		"id":   info.ID,
		"name": info.Name,
	})
	return info, nil
}

// ReadInstance returns what <root>/<id>/instance.json says about one instance.
// A directory without that file still answers, with just its id: an instance
// that has never run has nothing recorded yet.
func ReadInstance(root, id string) InstanceInfo {
	dir := filepath.Join(root, id)
	entry := readJSONFile(filepath.Join(dir, "instance.json"))
	name, _ := entry["name"].(string)
	return InstanceInfo{
		ID:        id,
		Name:      name,
		Dir:       dir,
		StartTime: int64(intField(entry, "start_time")),
		EndTime:   int64(intField(entry, "end_time")),
	}
}

// FillInstance records fields in <root>/<id>/instance.json that are not there
// yet, keeping everything already written — including any of these fields the
// instance has already recorded for itself. It is for filling a gap, not for
// overruling what the file says: the window frame arrives this way when it is
// carried over from settings.json, where it used to be kept.
func FillInstance(root, id string, fields map[string]any) {
	path := filepath.Join(root, id, "instance.json")
	entry := readJSONFile(path)
	changed := false
	for key, value := range fields {
		if _, taken := entry[key]; taken {
			continue
		}
		entry[key] = value
		changed = true
	}
	if changed {
		writeJSON(path, entry)
	}
}

// ListInstances returns every pb-* directory under root, most recently run
// first, with the never-run ones after them.
func ListInstances(root string) []InstanceInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var instances []InstanceInfo
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), InstancePrefix) {
			continue
		}
		instances = append(instances, ReadInstance(root, entry.Name()))
	}
	sort.Slice(instances, func(a, b int) bool {
		if instances[a].StartTime != instances[b].StartTime {
			return instances[a].StartTime > instances[b].StartTime
		}
		return instances[a].ID < instances[b].ID
	})
	return instances
}

// SetInstanceID points <root>/INSTANCE at id, which is how a machine is moved
// from one instance to another: it is the only thing that says which
// directory is in use.
func SetInstanceID(root, id string) error {
	if !strings.HasPrefix(id, InstancePrefix) || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("%q is not an instance id", id)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, instancePointer), []byte(id+"\n"), 0o644)
}
