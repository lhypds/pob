package applog

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestInstanceSinkReceivesAppMessages(t *testing.T) {
	Init(t.TempDir())
	var got []string
	SetInstanceSink(func(message string) { got = append(got, message) })

	Log("one")
	Logf("two %d", 2)

	want := []string{"one", "two 2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("instance sink got %q, want %q", got, want)
	}
}

func TestInitClearsThePreviousInstanceSink(t *testing.T) {
	Init(t.TempDir())
	called := false
	SetInstanceSink(func(string) { called = true })
	Init(t.TempDir())

	Log("after reinitialising")
	if called {
		t.Error("Init retained the previous instance sink")
	}
}

func TestAppLogTimestampHasFixedMicrosecondWidth(t *testing.T) {
	root := t.TempDir()
	Init(root)
	Log("fixed width")

	data, err := os.ReadFile(root + "/app.log")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	want := regexp.MustCompile(`^\[[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z\] fixed width$`)
	if !want.MatchString(line) {
		t.Errorf("app.log timestamp is not fixed-width microseconds: %q", line)
	}
}
