package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/spf13/cobra"
)

func newRunCmd(rf *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "run <request>",
		Short: "Suggest and execute a command from a natural language request",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := strings.Join(args, " ")
			if strings.TrimSpace(input) == "" {
				return errors.New("empty request")
			}
			s, err := newSession(rf.cfg)
			if err != nil {
				return err
			}
			defer s.close()
			s.setAutoYes(rf.autoYes)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			s.SetInput(NewBufioReader(cmd.InOrStdin()))
			resp, err := askWithFollowUp(ctx, s, "run", input, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				if errors.Is(err, errCancelQuestion) {
					fmt.Fprintln(cmd.OutOrStdout(), "(cancelled)")
					return nil
				}
				return err
			}
			if resp.Intent != prompt.IntentRunCommand {
				return nil
			}
			if rf.cfg.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "(dry-run: command not executed)")
				return nil
			}
			return runCommandWithCorrection(ctx, s, rf, resp, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

// evaluatePolicy — обёртка над policy.Evaluate, удобная для render-слоя.
func evaluatePolicy(resp prompt.Response, cfg *config.Config) policy.Decision {
	if resp.Intent != prompt.IntentRunCommand {
		return policy.Decision{Allowed: true, Risk: prompt.RiskLow}
	}
	var userDanger, userSuspicious []string
	if cfg != nil {
		userDanger = cfg.DangerPatterns
		userSuspicious = cfg.SuspiciousPatterns
	}
	dec := policy.Evaluate(resp.Command, resp.Risk, userDanger, userSuspicious)
	if dec.Allowed && cfg != nil && allowlisted(resp.Command, cfg.Allowlist) {
		dec.Risk = prompt.RiskLow
	}
	return dec
}

// allowlisted сообщает, входит ли команда в список всегда-безопасных.
// Сопоставление по префиксу с границей слова: "git status" покрывает
// "git status -s", но не "git statusremote".
func allowlisted(cmd string, list []string) bool {
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if cmd == entry || strings.HasPrefix(cmd, entry+" ") {
			return true
		}
	}
	return false
}

// directDecision возвращает "решение" для команд, запущенных вручную через
// '!' или в shell-режиме — они минуют политику, поэтому помечаются как
// разрешённые с низким риском и причиной direct.
func directDecision() policy.Decision {
	return policy.Decision{Allowed: true, Risk: prompt.RiskLow, Reason: "direct"}
}

// hasExecutableCommand сообщает, содержит ли ответ команду, которую нужно
// выполнить: явный run_command, либо explain/ask_clarification с командой.
func hasExecutableCommand(resp prompt.Response) bool {
	if resp.Intent == prompt.IntentRunCommand {
		return true
	}
	return (resp.Intent == prompt.IntentExplain || resp.Intent == prompt.IntentAskClarification) && resp.Command != ""
}
