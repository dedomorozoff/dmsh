package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/diag"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

func TestErrorHint_UnifiedCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{name: "no local model", err: llm.ErrNoLocalModel, want: []string{"model is not downloaded", "dmsh model download"}},
		{name: "no cgo", err: llm.ErrNotBuiltWithCGO, want: []string{"local inference is unavailable", "-tags llama"}},
		{name: "proxy", err: errors.New("proxy: invalid proxy address"), want: []string{"proxy is unavailable", "proxy settings"}},
		{name: "auth", err: errors.New("llm: pollinations HTTP 401: unauthorized"), want: []string{"POLLINATIONS_API_KEY", "remote_api_key"}},
		{name: "ollama down", err: errors.New("llm: ollama request to http://127.0.0.1:11434 failed: dial tcp: connection refused"), want: []string{"Ollama is not responding", "ollama serve"}},
		{name: "no network", err: errors.New("dial tcp: lookup api.example.com: no such host"), want: []string{"no network connection", "network/proxy/provider"}},
		{name: "config", err: errors.New("parse config /tmp/c.json: bad"), want: []string{"check configuration", "config show"}},
		{name: "usage", err: errors.New("requires at least 1 arg(s), only received 0"), want: []string{"--help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorHint(tt.err)
			for _, sub := range tt.want {
				if !strings.Contains(got, sub) {
					t.Fatalf("ErrorHint(%v)=%q, want substring %q", tt.err, got, sub)
				}
			}
		})
	}
}

// The CLI and the provider layer must print the same phrase for the same
// cause; pin the exact wording so a local edit cannot drift again.
func TestErrorHintUsesDiagWording(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("llm: pollinations HTTP 401: unauthorized"), diag.HintAPIKeyRequired},
		{errors.New("llm: ollama request failed: dial tcp: connection refused"), diag.HintOllamaNotRunning},
		{errors.New("dial tcp: lookup api.example.com: no such host"), diag.HintNoNetwork},
		{errors.New("proxy: invalid address"), diag.HintProxy},
		{llm.ErrNoLocalModel, diag.HintLocalModelMissing},
		{llm.ErrNotBuiltWithCGO, diag.HintNoCGO},
	}
	for _, tc := range cases {
		if got := ErrorHint(tc.err); got != tc.want {
			t.Errorf("ErrorHint(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestErrorKindAndExitCode(t *testing.T) {
	if ErrorKind(llm.ErrNoLocalModel) != "provider" || ErrorExitCode(llm.ErrNoLocalModel) != 4 {
		t.Fatal("ErrNoLocalModel should classify as provider/4")
	}
	if ErrorKind(errors.New("parse config: bad")) != "config" || ErrorExitCode(errors.New("parse config: bad")) != 3 {
		t.Fatal("config parse should classify as config/3")
	}
	if ErrorKind(errors.New("unknown flag: --x")) != "usage" || ErrorExitCode(errors.New("unknown flag: --x")) != 2 {
		t.Fatal("usage should classify as usage/2")
	}
	if ErrorKind(&ExitError{Code: 5}) != "exec" || ErrorExitCode(&ExitError{Code: 5}) != 5 {
		t.Fatal("ExitError should classify as exec with its code")
	}
}
