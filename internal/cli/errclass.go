package cli

import (
	"errors"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/llm"
)

const (
	exitGeneral  = 1
	exitUsage    = 2
	exitConfig   = 3
	exitProvider = 4
)

// ErrorHint returns a user-facing hint for err. It unifies common failure cases
// (no network, Ollama not responding, missing model, proxy issues, auth/config)
// so callers can present one consistent suggestion.
func ErrorHint(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, llm.ErrNoLocalModel):
		return "model is not downloaded: run `dmsh model download` or switch provider with `/setup`; run `dmsh doctor`"
	case errors.Is(err, llm.ErrNotBuiltWithCGO):
		return "local inference is unavailable in this build: rebuild with `-tags llama` or use a remote provider; run `dmsh doctor`"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case IsUsageError(err):
		return "run with --help for usage"
	case IsConfigError(err):
		return "check configuration with `dmsh config show` or `dmsh doctor`"
	case isProxyError(msg):
		return "proxy is unavailable or misconfigured: check proxy settings with `dmsh config show` or `dmsh doctor`"
	case isAuthError(msg):
		return "set POLLINATIONS_API_KEY or configure remote_api_key; run `dmsh doctor`"
	case isOllamaUnavailable(msg):
		return "Ollama is not responding: start it with `ollama serve` and check remote_base_url; run `dmsh doctor`"
	case isNoNetworkError(msg):
		return "no network connection or remote endpoint unreachable: check network/proxy/provider settings; run `dmsh doctor`"
	case IsProviderError(err):
		return "check provider/model/network with `dmsh config show` or `dmsh doctor`"
	default:
		return "run `dmsh doctor` for diagnostics"
	}
}

// ErrorKind classifies err for machine-readable output.
func ErrorKind(err error) string {
	if err == nil {
		return ""
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return "exec"
	}
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		return "exec"
	}
	switch {
	case IsUsageError(err):
		return "usage"
	case IsConfigError(err):
		return "config"
	case IsProviderError(err):
		return "provider"
	default:
		return "general"
	}
}

// ErrorExitCode maps err to a stable CLI exit code.
func ErrorExitCode(err error) int {
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
	case IsUsageError(err):
		return exitUsage
	case IsConfigError(err):
		return exitConfig
	case IsProviderError(err):
		return exitProvider
	default:
		return exitGeneral
	}
}

// IsUsageError reports argument/flag/command usage errors.
func IsUsageError(err error) bool {
	if err == nil {
		return false
	}
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

// IsConfigError reports configuration load/parse/validation/path errors.
func IsConfigError(err error) bool {
	if err == nil {
		return false
	}
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

// IsProviderError reports provider, model, and network/API errors.
func IsProviderError(err error) bool {
	if err == nil {
		return false
	}
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

func isProxyError(msg string) bool {
	return strings.Contains(msg, "proxy")
}

func isAuthError(msg string) bool {
	for _, p := range []string{"api key", "unauthorized", " 401", "401 ", "http 401", "forbidden", " 403", "403 ", "http 403"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func isNoNetworkError(msg string) bool {
	for _, p := range []string{"no such host", "dial tcp", "i/o timeout", "context deadline exceeded", "tls handshake", "network is unreachable", "connection refused", "connection reset", "timeout"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func isOllamaUnavailable(msg string) bool {
	if !strings.Contains(msg, "ollama") {
		return false
	}
	for _, p := range []string{"connection refused", "dial tcp", "i/o timeout", "context deadline exceeded", "no such host", "request to", "failed"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
