package applog

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestInstanceSinkReceivesEveryLevel(t *testing.T) {
	Init(t.TempDir())
	var got []string
	SetInstanceSink(func(level, message string) { got = append(got, level+" "+message) })

	Log("one")
	Logf("two %d", 2)
	Event("started")
	Errorf("broke: %v", "why")

	want := []string{"INFO one", "INFO two 2", "INFO started", "ERROR broke: why"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("instance sink got %q, want %q", got, want)
	}
}

func TestInitClearsThePreviousInstanceSink(t *testing.T) {
	Init(t.TempDir())
	called := false
	SetInstanceSink(func(string, string) { called = true })
	Init(t.TempDir())

	Log("after reinitialising")
	if called {
		t.Error("Init retained the previous instance sink")
	}
}

// app.log is the short record: the app and its instances coming up and going
// down, and what failed. Detail is the instance log's job.
func TestAppLogKeepsOnlyEventsAndErrors(t *testing.T) {
	root := t.TempDir()
	Init(root)

	Log("a step ran")
	Event("pob-core started")
	Error("listen failed")

	lines := readAppLog(t, root)
	if len(lines) != 2 {
		t.Fatalf("app.log has %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.HasSuffix(lines[0], "] pob-core started") {
		t.Errorf("first app.log line is %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "] ERROR listen failed") {
		t.Errorf("second app.log line is %q", lines[1])
	}
}

// Six fractional digits and an offset that is always written, so rows line up
// under each other — and local time rather than UTC, so a row reads in the time
// whoever is at the machine watched it happen.
func TestAppLogTimestampHasFixedMicrosecondWidth(t *testing.T) {
	root := t.TempDir()
	Init(root)
	Event("fixed width")

	lines := readAppLog(t, root)
	want := regexp.MustCompile(`^\[[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}[+-][0-9]{2}:[0-9]{2}\] fixed width$`)
	if len(lines) != 1 || !want.MatchString(lines[0]) {
		t.Errorf("app.log timestamp is not fixed-width microseconds with a zone offset: %q", lines)
	}
}

func TestAppLogTimestampIsLocalTime(t *testing.T) {
	root := t.TempDir()
	Init(root)
	before := time.Now()
	Event("local time")

	lines := readAppLog(t, root)
	if len(lines) != 1 {
		t.Fatalf("app.log has %d lines, want 1: %q", len(lines), lines)
	}
	stamp := lines[0][1:strings.Index(lines[0], "]")]
	logged, err := time.Parse(TimeLayout, stamp)
	if err != nil {
		t.Fatalf("cannot parse %q with %q: %v", stamp, TimeLayout, err)
	}
	// A UTC stamp written where a local one was meant is out by the zone's
	// offset; parsing it back and comparing catches that wherever the test runs.
	if d := logged.Sub(before); d < 0 || d > time.Minute {
		t.Errorf("app.log stamped %s, which is %s from now — not the machine's own clock", stamp, d)
	}
	if _, offset := logged.Zone(); offset != zoneOffset(before) {
		t.Errorf("app.log stamped offset %ds, want the machine's %ds", offset, zoneOffset(before))
	}
}

func zoneOffset(at time.Time) int {
	_, offset := at.Local().Zone()
	return offset
}

func readAppLog(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(root + "/app.log")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
