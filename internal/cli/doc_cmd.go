package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/rag"
	"github.com/spf13/cobra"
)

// docsDBPath keeps the index next to the other dmsh state files.
func docsDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dmsh", "docs.db"), nil
}

// openDocStore rebuilds the index from root. Project docs are small, so a
// fresh index is cheaper than tracking mtimes and avoids stale answers.
func openDocStore(root string) (*rag.Store, int, error) {
	path, err := docsDBPath()
	if err != nil {
		return nil, 0, err
	}
	st, err := rag.Open(path)
	if err != nil {
		return nil, 0, err
	}
	n, err := st.Index(root)
	if err != nil {
		_ = st.Close()
		return nil, 0, err
	}
	return st, n, nil
}

func formatDocHits(hits []rag.Hit) string {
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s:%d (score %.1f)\n", i+1, h.File, h.Line, h.Score)
		lines := strings.Split(h.Text, "\n")
		if len(lines) > 3 {
			lines = lines[:3]
		}
		for _, line := range lines {
			fmt.Fprintf(&b, "   %s\n", line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// newDocCmd exposes retrieval directly; answers can be synthesised by the
// model with /doc in the TUI, which adds the same hits to the transcript.
func newDocCmd(rf *rootFlags) *cobra.Command {
	var dir string
	var limit int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doc <query>",
		Short: "Search the project documentation with file:line references",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("empty query")
			}
			st, _, err := openDocStore(dir)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			hits, err := st.Search(query, limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				return json.NewEncoder(out).Encode(hits)
			}
			if len(hits) == 0 {
				fmt.Fprintln(out, "No documentation matches.")
				return nil
			}
			fmt.Fprintln(out, formatDocHits(hits))
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "documentation root")
	cmd.Flags().IntVar(&limit, "limit", 5, "maximum number of matches")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print matches as JSON")
	return cmd
}

// docAnswer returns a compact context block for a retrieval-augmented answer.
func docAnswer(st *rag.Store, query string, limit int) (string, error) {
	hits, err := st.Search(query, limit)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "", nil
	}
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "[%s:%d]\n%s\n\n", h.File, h.Line, h.Text)
	}
	return b.String(), nil
}
