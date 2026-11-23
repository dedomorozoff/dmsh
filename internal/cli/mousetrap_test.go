package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestMousetrapHelpDisabled(t *testing.T) {
	if cobra.MousetrapHelpText != "" {
		t.Fatalf("Cobra mousetrap must be disabled for the console build, got %q", cobra.MousetrapHelpText)
	}
}
