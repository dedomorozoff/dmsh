package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/cli"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

const (
	exitGeneral  = 1
	exitUsage    = 2
	exitConfig   = 3
	exitProvider = 4
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
		code := exitCode(err)
		if wantsJSON(args) {
			payload := map[string]any{
				"ok":    false,
				"error": err.Error(),
				"kind":  errorKind(err),
				"hint":  errorHint(err),
			}
			if code != exitGeneral {
				payload["exit_code"] = code
			}
			_ = json.NewEncoder(stdout).Encode(payload)
			return code
		}
		fmt.Fprintln(stderr, err)
		if hint := errorHint(err); hint != "" && !strings.Contains(err.Error(), "dmsh doctor") {
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

func errorHint(err error) string {
	switch {
	case errors.Is(err, llm.ErrNoLocalModel):
		return "download a model with `dmsh model download` or switch provider with `/setup`; run `dmsh doctor`"
	case errors.Is(err, llm.ErrNotBuiltWithCGO):
		return "rebuild with `-tags llama` for local inference or use a remote provider; run `dmsh doctor`"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case isUsageError(err):
		return "run with --help for usage"
	case isConfigError(err):
		return "check configuration with `dmsh config show` or `dmsh doctor`"
	case strings.Contains(msg, "proxy"):
		return "check proxy settings with `dmsh config show` or `dmsh doctor`"
	case strings.Contains(msg, "api key"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, " 401"), strings.Contains(msg, "401 "),
		strings.Contains(msg, "forbidden"), strings.Contains(msg, " 403"), strings.Contains(msg, "403 "):
		return "set POLLINATIONS_API_KEY or configure remote_api_key; run `dmsh doctor`"
	case isProviderError(err):
		return "check provider/model/network with `dmsh config show` or `dmsh doctor`"
	default:
		return "run `dmsh doctor` for diagnostics"
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		if code := ec.ExitCode(); code > 0 {
			return code
		}
		return exitGeneral
	}
	switch {
	case isUsageError(err):
		return exitUsage
	case isConfigError(err):
		return exitConfig
	case isProviderError(err):
		return exitProvider
	default:
		return exitGeneral
	}
}

// isUsageError reports argument/flag/command usage errors.
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
		"accepts at least",
		"accepts at most",
		"accepts ",
		"if any flags in the group",
		"interactive mode is not supported with --json",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// isConfigError reports configuration load/parse/validation/path errors.
func isConfigError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, p := range []string{
		"parse config",
		"read config",
		"marshal config",
		"config path",
		"config:",
		"invalid mode ",
		"invalid remote_base_url",
		"out of range",
		"invalid regex pattern",
		"failed to save config",
		"failed to load config",
		"config is invalid",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// isProviderError reports provider, model, and network/API errors.
func isProviderError(err error) bool {
	if errors.Is(err, llm.ErrNoLocalModel) || errors.Is(err, llm.ErrNotBuiltWithCGO) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, p := range []string{
		"unknown provider",
		"no local gguf",
		"model not found",
		"model_path",
		"llm:",
		"api key",
		"unauthorized",
		"forbidden",
		" 401",
		"401 ",
		"http 401",
		" 403",
		"403 ",
		"http 403",
		"connection refused",
		"no such host",
		"dial tcp",
		"i/o timeout",
		"context deadline exceeded",
		"tls handshake",
		"endpoint",
		"ollama",
		"pollinations",
		"provider=",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func errorKind(err error) string {
	if err == nil {
		return ""
	}
	var ee *cli.ExitError
	if errors.As(err, &ee) {
		return "exec"
	}
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		return "exec"
	}
	switch {
	case isUsageError(err):
		return "usage"
	case isConfigError(err):
		return "config"
	case isProviderError(err):
		return "provider"
	default:
		return "general"
	}
}
