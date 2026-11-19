package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/cli"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

func main() {
	cmd := cli.NewRootCmd()
	if err := cmd.Execute(); err != nil {
		if wantsJSON(os.Args[1:]) {
			payload := map[string]any{
				"ok":    false,
				"error": err.Error(),
				"hint":  errorHint(err),
			}
			if code := exitCode(err); code != 1 {
				payload["exit_code"] = code
			}
			_ = json.NewEncoder(os.Stdout).Encode(payload)
			os.Exit(exitCode(err))
		}
		fmt.Fprintln(os.Stderr, err)
		if hint := errorHint(err); hint != "" && !strings.Contains(err.Error(), "dmsh doctor") {
			fmt.Fprintln(os.Stderr, "hint:", hint)
		}
		os.Exit(exitCode(err))
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

func errorHint(err error) string {
	switch {
	case errors.Is(err, llm.ErrNoLocalModel):
		return "download a model with `dmsh model download` or switch provider with `/setup`; run `dmsh doctor`"
	case errors.Is(err, llm.ErrNotBuiltWithCGO):
		return "rebuild with `-tags llama` for local inference or use a remote provider; run `dmsh doctor`"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "proxy"):
		return "check proxy settings with `dmsh config show` or `dmsh doctor`"
	case strings.Contains(msg, "config"):
		return "run `dmsh doctor` for diagnostics"
	case strings.Contains(msg, "api key"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"):
		return "set POLLINATIONS_API_KEY or configure remote_api_key; run `dmsh doctor`"
	case strings.Contains(msg, "model"):
		return "run `dmsh doctor` to check model/provider configuration"
	default:
		return "run `dmsh doctor` for diagnostics"
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if isUsageError(err) {
		return 2
	}
	return 1
}

// isUsageError reports argument/flag/command usage errors that should map to exit code 2.
func isUsageError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, p := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"invalid argument",
		"requires at least",
		"requires at most",
		"accepts",
		"if any flags in the group",
		"interactive mode is not supported with --json",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
