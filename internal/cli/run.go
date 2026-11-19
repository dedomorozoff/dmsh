package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/spf13/cobra"
)

func newRunCmd(rf *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
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
			if !rf.jsonOut {
				s.printNotice(cmd.ErrOrStderr())
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			out, errW := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if rf.jsonOut {
				out = io.Discard
			} else {
				s.SetInput(NewBufioReader(cmd.InOrStdin()))
			}
			resp, err := askWithFollowUp(ctx, s, "run", input, out, errW)
			if err != nil {
				if errors.Is(err, errCancelQuestion) {
					if rf.jsonOut {
						return writeJSON(cmd.OutOrStdout(), map[string]any{"cancelled": true})
					}
					fmt.Fprintln(cmd.OutOrStdout(), "(cancelled)")
					return nil
				}
				return err
			}
			dec := evaluatePolicy(resp, &rf.cfg)
			if rf.jsonOut {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"response": resp,
					"policy": map[string]any{
						"allowed": dec.Allowed,
						"risk":    dec.Risk,
						"reason":  dec.Reason,
					},
					"executable": resp.Intent == prompt.IntentRunCommand && resp.Command != "",
					"executed":   false,
				})
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
	cmd.Flags().BoolVar(&rf.jsonOut, "json", false, "print the result as JSON to stdout (no banners/colors; diagnostics to stderr)")
	return cmd
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
