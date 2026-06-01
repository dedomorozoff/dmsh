package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/nlsh/internal/executor"
	"github.com/dedomorozoff/nlsh/internal/prompt"
	"github.com/spf13/cobra"
)

// runOneShot обрабатывает одиночный запрос без подкоманды:
// nlsh "покажи последние 20 строк лога"
// Поддерживает pipe: cat error.log | nlsh "что здесь не так?"
func runOneShot(cmd *cobra.Command, rf *rootFlags, input string) error {
	s, err := newSession(rf.cfg)
	if err != nil {
		return err
	}
	defer s.close()

	// Читаем stdin если он не TTY (pipe-режим)
	stdin := cmd.InOrStdin()
	if f, ok := stdin.(*os.File); ok && !isTerminal(f) {
		data, readErr := io.ReadAll(f)
		if readErr == nil && len(data) > 0 {
			const maxStdin = 8 * 1024 // 8 КБ
			text := strings.TrimSpace(string(data))
			if len(text) > maxStdin {
				text = text[:maxStdin] + "\n...[truncated]"
			}
			s.stdinCtx = text
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := askWithFollowUp(ctx, s, "run", input, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		if errors.Is(err, errCancelQuestion) {
			fmt.Fprintln(cmd.OutOrStdout(), "(cancelled)")
			return nil
		}
		return err
	}

	dec := evaluatePolicy(resp, &rf.cfg)
	_ = dec

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
		if !confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "execute?") {
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

	res := executor.RunInteractive(ctx, rf.cfg.Shell, resp.Command)
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

