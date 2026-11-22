package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage named dmsh sessions",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List named sessions with saved turns",
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := listNamedSessions()
			if err != nil {
				return err
			}
			if len(names) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No named sessions found. Use `dmsh --session <name>` to start one.")
				return nil
			}
			for _, n := range names {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named session's saved turns",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := sanitizeSessionName(args[0])
			if name == "" {
				return fmt.Errorf("invalid session name %q", args[0])
			}
			path := sessionFileNamed(name)
			if path == "" {
				return fmt.Errorf("cannot determine session path")
			}
			if err := os.Remove(path); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("session %q not found", name)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted session %q\n", name)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "path <name>",
		Short: "Show the turns file path for a named session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := sanitizeSessionName(args[0])
			if name == "" {
				return fmt.Errorf("invalid session name %q", args[0])
			}
			path := sessionFileNamed(name)
			if path == "" {
				return fmt.Errorf("cannot determine session path")
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	})

	return cmd
}

func listNamedSessions() ([]string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	sdir := filepath.Join(dir, "dmsh", "sessions")
	entries, err := os.ReadDir(sdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, filepath.Ext(name)))
	}
	sort.Strings(names)
	return names, nil
}
