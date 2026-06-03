package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dedomorozoff/nlsh/internal/config"
	"github.com/dedomorozoff/nlsh/internal/policy"
	"github.com/dedomorozoff/nlsh/internal/prompt"
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
	if cfg == nil {
		return policy.Evaluate(resp.Command, resp.Risk, nil, nil)
	}
	return policy.Evaluate(resp.Command, resp.Risk, cfg.DangerPatterns, cfg.SuspiciousPatterns)
}
