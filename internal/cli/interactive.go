package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// runInteractive запускает REPL как дефолтное поведение root-команды.
func runInteractive(cmd *cobra.Command, rf *rootFlags) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintln(out, "╔══════════════════════════════════════════╗")
	fmt.Fprintln(out, "║          .nlsh — Natural Shell           ║")
	fmt.Fprintln(out, "║   Type commands in natural language      ║")
	fmt.Fprintln(out, "║   Example: show me all files             ║")
	fmt.Fprintln(out, "║   Alt+1=AI  Alt+2=Help  Alt+3=Shell      ║")
	fmt.Fprintln(out, "║   Type /help for commands.               ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════╝")
	fmt.Fprintln(out, "")

	s, err := newSession(rf.cfg)
	if err != nil {
		fmt.Fprintln(errOut, "")
		fmt.Fprintf(errOut, "  ✗ Model error: %v\n", err)
		fmt.Fprintln(errOut, "  → Run: nlsh model download")
		fmt.Fprintln(errOut, "")
		return err
	}
	defer s.close()

	in := cmd.InOrStdin()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if isTTY := isTerminal(in); isTTY {
		return replLoopReadline(ctx, s, rf, out, errOut)
	}
	return replLoop(ctx, s, rf, in, out, errOut)
}
