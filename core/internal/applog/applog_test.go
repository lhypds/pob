package applog

import (
	"reflect"
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
