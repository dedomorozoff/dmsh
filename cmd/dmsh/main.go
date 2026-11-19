package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dedomorozoff/dmsh/internal/cli"
)

func main() {
	cmd := cli.NewRootCmd()
	if err := cmd.Execute(); err != nil {
		if wantsJSON(os.Args[1:]) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"ok":    false,
				"error": err.Error(),
				"hint":  "run `dmsh doctor` for diagnostics",
			})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "--json=true" {
			return true
		}
		// --json=false explicitly disables JSON output.
		if a == "--json=false" {
			return false
		}
	}
	return false
}
