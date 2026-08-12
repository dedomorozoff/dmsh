package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/nlsh/internal/config"
	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	var limit int
	var query string

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show command history",
		Long:  "Display commands executed in previous and current sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			entries, err := loadHistoryEntries(cfg.HistoryFile)
			if err != nil {
				return err
			}

			// Filter if search query is specified
			if query != "" {
				var filtered []HistoryEntry
				for _, e := range entries {
					if strings.Contains(strings.ToLower(e.Command), strings.ToLower(query)) {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}

			// Limit the output
			if limit > 0 && len(entries) > limit {
				entries = entries[len(entries)-limit:]
			}

			if len(entries) == 0 {
				_, _ = fmt.Fprintln(out, "No history found.")
				return nil
			}

			_, _ = fmt.Fprintln(out, "=== Command History ===")
			for i, e := range entries {
				tStr := e.Timestamp.Format("2006-01-02 15:04:05")
				fmt.Fprintf(out, "%4d  [%s] (%-6s)  %s\n", i+1, tStr, e.Source, e.Command)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Limit number of entries shown")
	cmd.Flags().StringVarP(&query, "grep", "g", "", "Filter history by query string")

	return cmd
}

func loadHistoryEntries(filepath string) ([]HistoryEntry, error) {
	if filepath == "" {
		return nil, nil
	}
	f, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open history file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

func printHistoryLimit(out io.Writer, filepath string, limit int) {
	entries, err := loadHistoryEntries(filepath)
	if err != nil {
		fmt.Fprintf(out, "Failed to load history: %v\n", err)
		return
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "No history found.")
		return
	}
	for i, e := range entries {
		fmt.Fprintf(out, "%4d  %s\n", i+1, e.Command)
	}
}
