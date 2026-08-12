package storage

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestInstanceLogTimestampsEveryContentRow(t *testing.T) {
	store := New(t.TempDir(), "pb-test", func() map[string]any { return nil }, func() string { return "" })
	store.LogInstance("PSL REQUEST CONTENT", "moveTo(10, 20)\nclick()")

	data, err := os.ReadFile(store.InstanceLogFile())
	if err != nil {
		t.Fatalf("reading instance.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("instance.log has %d rows, want 2:\n%s", len(lines), data)
	}
	stamp := regexp.MustCompile(`^\[[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z\] PSL REQUEST CONTENT `)
	for i, line := range lines {
		if !stamp.MatchString(line) {
			t.Errorf("row %d is not timestamped and labelled: %q", i+1, line)
		}
	}
	if !strings.HasSuffix(lines[0], "moveTo(10, 20)") || !strings.HasSuffix(lines[1], "click()") {
		t.Errorf("raw content rows were not preserved:\n%s", data)
	}
}

func TestInstanceLogAppendsAcrossEvents(t *testing.T) {
	store := New(t.TempDir(), "pb-test", func() map[string]any { return nil }, func() string { return "" })
	store.LogInstance("INSTANCE START", "id=pb-test")
	store.LogInstancef("MACRO STOP", "session=%s status=%q", "123", "completed")

	data, err := os.ReadFile(store.InstanceLogFile())
	if err != nil {
		t.Fatalf("reading instance.log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "INSTANCE START id=pb-test") ||
		!strings.Contains(log, `MACRO STOP session=123 status="completed"`) {
		t.Errorf("instance.log did not append both events:\n%s", log)
	}
}
