package cli

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

func runInteractive(cmd *cobra.Command, rf *rootFlags) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintln(out, "╔══════════════════════════════════════════╗")
	fmt.Fprintln(out, "║      dmsh — Direct Model Shell           ║")
	fmt.Fprintln(out, "║   Type commands in natural language      ║")
	fmt.Fprintln(out, "║   Example: show me all files             ║")
	fmt.Fprintln(out, "║   Type /help for commands.               ║")
	fmt.Fprintln(out, "╚══════════════════════════════════════════╝")
	fmt.Fprintln(out, "")

	s, err := newSession(rf.cfg)
	if err != nil {
		fmt.Fprintf(errOut, "%sModel error: %v%s\n", colorRed, err, colorReset)
		fmt.Fprintln(errOut, "Run: dmsh model")
		fmt.Fprintln(errOut, "")
		return err
	}
	defer s.close()

	m := NewTuiModel(rf, s)
	p := tea.NewProgram(m)
	_, err = p.Run()
	if errOut != nil && err != nil {
		fmt.Fprintf(errOut, "%s%s\n", colorRed, err)
	}
	return err
}
