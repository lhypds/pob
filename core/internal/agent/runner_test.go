package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"pob/core/internal/config"
	"pob/core/internal/psl"
	"pob/core/internal/storage"
)

func TestStopAndWaitLetsTheMacroFinishLoggingBeforeShutdown(t *testing.T) {
	root := t.TempDir()
	cfg := config.New(root, "pb-test")
	store := storage.New(root, "pb-test", cfg.SettingsDict, cfg.Macro)
	runner := NewRunner(cfg, store, psl.Compiler{}, nil)

	ctx := runner.start()
	runner.setCurrentSession("session-1")
	go func() {
		<-ctx.Done()
		runner.finish()
	}()

	if !runner.StopAndWait(time.Second) {
		t.Fatal("StopAndWait timed out")
	}
	if runner.Running() {
		t.Error("runner is still running after StopAndWait")
	}
	data, err := os.ReadFile(store.InstanceLogFile())
	if err != nil {
		t.Fatalf("reading instance.log: %v", err)
	}
	if !strings.Contains(string(data), `MACRO STOP REQUEST session=session-1 reason="instance shutdown"`) {
		t.Errorf("instance.log has no shutdown stop request:\n%s", data)
	}
}
