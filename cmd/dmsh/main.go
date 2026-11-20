package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI with provided args and writers, returning the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := cli.NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		code := cli.ErrorExitCode(err)
		if wantsJSON(args) {
			payload := map[string]any{
				"ok":    false,
				"error": err.Error(),
				"kind":  cli.ErrorKind(err),
				"hint":  cli.ErrorHint(err),
			}
			if code != 1 {
				payload["exit_code"] = code
			}
			_ = json.NewEncoder(stdout).Encode(payload)
			return code
		}
		fmt.Fprintln(stderr, err)
		if hint := cli.ErrorHint(err); hint != "" && !strings.Contains(err.Error(), "dmsh doctor") {
			fmt.Fprintln(stderr, "hint:", hint)
		}
		return code
	}
	return 0
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
