package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNotifyFirstRunOnce(t *testing.T) {
	cmd := &cobra.Command{Use: "dmsh"}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	rf := &rootFlags{firstRun: true}
	rf.notifyFirstRun(cmd)
	got := errBuf.String()
	if !strings.Contains(got, "dry-run is on by default") || !strings.Contains(got, "--dry-run=false") {
		t.Fatalf("notice should explain the safe default and the opt-out:\n%s", got)
	}
	rf.notifyFirstRun(cmd)
	if errBuf.String() != got {
		t.Fatal("the first-run notice must not repeat")
	}
}

func TestNotifyFirstRunSkippedInJSON(t *testing.T) {
	cmd := &cobra.Command{Use: "dmsh"}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	rf := &rootFlags{firstRun: true, jsonOut: true}
	rf.notifyFirstRun(cmd)
	if errBuf.Len() != 0 {
		t.Fatalf("JSON output must stay clean, got %q", errBuf.String())
	}
}
