package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newAskCmd(rf *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask <query>",
		Short: "Explain how to do something without executing",
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
			if rf.jsonOut {
				s.notice = ""
			} else {
				s.printNotice(cmd.ErrOrStderr())
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			out, errW := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if rf.jsonOut {
				// JSON-контракт на stdout: стрим, баннеры и вопросы не пишем.
				out = io.Discard
			} else {
				s.SetInput(NewBufioReader(cmd.InOrStdin()))
			}
			resp, err := askWithFollowUp(ctx, s, "ask", input, out, errW)
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
			_ = evaluatePolicy(resp, &rf.cfg)
			if rf.jsonOut {
				return writeJSON(cmd.OutOrStdout(), resp)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&rf.jsonOut, "json", false, "print the response as JSON to stdout (no banners/colors; diagnostics to stderr)")
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
