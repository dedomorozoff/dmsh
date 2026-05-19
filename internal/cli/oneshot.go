package cli

import (
	"context"
	"fmt"

	"github.com/dedomorozoff/nlsh/internal/executor"
	"github.com/dedomorozoff/nlsh/internal/prompt"
	"github.com/spf13/cobra"
)

// runOneShot обрабатывает одиночный запрос без подкоманды:
// nlsh "покажи последние 20 строк лога"
func runOneShot(cmd *cobra.Command, rf *rootFlags, input string) error {
	s, err := newSession(rf.cfg)
	if err != nil {
		return err
	}
	defer s.close()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	s.SetInput(NewBufioReader(cmd.InOrStdin()))
	resp, err := askWithFollowUp(ctx, s, "run", input, cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	dec := evaluatePolicy(resp)
	renderResponse(cmd.OutOrStdout(), resp, dec)

	if resp.Intent != prompt.IntentRunCommand {
		return nil
	}
	if rf.cfg.DryRun {
		fmt.Fprintln(cmd.OutOrStdout(), "(dry-run: command not executed)")
		return nil
	}
	if !dec.Allowed {
		fmt.Fprintln(cmd.OutOrStdout(), "(command blocked by security policy)")
		return nil
	}
	if dec.Risk != prompt.RiskLow || resp.NeedsConfirmation {
		ok, err := confirm(s.input, cmd.OutOrStdout(), &s.cfg, "execute?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "(cancelled)")
			return nil
		}
	}
	if handled, _, err := runBuiltin(resp.Command, cmd.OutOrStdout(), cmd.ErrOrStderr(), s.recent); handled {
		if err != nil {
			return err
		}
		s.addRecent(resp.Command)
		return nil
	}

	res := executor.Run(ctx, rf.cfg.Shell, resp.Command)
	s.addRecent(resp.Command)
	if res.Stdout != "" {
		fmt.Fprint(cmd.OutOrStdout(), res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprint(cmd.ErrOrStderr(), res.Stderr)
	}
	if res.Err != nil {
		return fmt.Errorf("exit %d: %w", res.ExitCode, res.Err)
	}
	return nil
}

