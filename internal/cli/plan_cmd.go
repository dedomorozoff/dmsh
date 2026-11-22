package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// newPlanCmd implements the line-based half of plan mode. The TUI offers the
// same flow through /plan and the existing /todo checklist window.
func newPlanCmd(rf *rootFlags) *cobra.Command {
	var run bool
	cmd := &cobra.Command{
		Use:   "plan <request>",
		Short: "Turn a request into a reviewed checklist and run it step by step",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := strings.TrimSpace(strings.Join(args, " "))
			if input == "" {
				return errors.New("empty request")
			}
			s, err := newSession(rf.cfg)
			if err != nil {
				return err
			}
			defer s.close()
			if err := s.setupDebug(rf.debug, rf.debugFile); err != nil {
				return err
			}
			s.setSessionName(rf.sessionName)
			s.setAutoYes(rf.autoYes)
			if !rf.jsonOut {
				s.printNotice(cmd.ErrOrStderr())
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			out, errW := cmd.OutOrStdout(), cmd.ErrOrStderr()
			streamOut := out
			if rf.jsonOut {
				streamOut = io.Discard
			}
			resp, _, err := s.askStream(ctx, "ask", planGenerationRequest(input), streamOut)
			if err != nil {
				return err
			}
			steps := parsePlanSteps(resp.Explanation)
			if len(steps) == 0 {
				return errors.New("the model returned no plan steps")
			}
			items, err := fillPlan(s, steps)
			if err != nil {
				return err
			}

			var executed, failed int
			if run {
				planOut := out
				if rf.jsonOut {
					planOut = io.Discard
					errW = io.Discard
				}
				executed, failed, err = executePlan(ctx, s, rf, planOut, errW)
				if err != nil {
					return err
				}
			}

			if rf.jsonOut {
				return writeJSON(out, map[string]any{
					"ok": true, "steps": steps, "executed": executed, "failed": failed,
				})
			}
			fmt.Fprintf(out, "%s=== Plan (%d steps) ===%s\n", colorBold+colorCyan, len(items), colorReset)
			for _, item := range items {
				fmt.Fprintf(out, "%2d. %s\n", item.ID, item.Task)
			}
			if !run {
				fmt.Fprintf(out, "%sReview with /todo in the TUI, then run with `/plan run` or `dmsh plan --run`.%s\n", gray, colorReset)
				return nil
			}
			fmt.Fprintf(out, "%splan finished: %d executed, %d failed%s\n", colorGray, executed, failed, colorReset)
			return nil
		},
	}
	cmd.Flags().BoolVar(&run, "run", false, "execute the plan step by step after generating it")
	cmd.Flags().BoolVar(&rf.jsonOut, "json", false, "print the plan as JSON to stdout (no banners/colors)")
	return cmd
}
