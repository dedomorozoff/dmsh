package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/cli"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

type exitErr struct{ code int }

func (e exitErr) Error() string { return "exit" }
func (e exitErr) ExitCode() int { return e.code }

func TestWantsJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"none", []string{"ask", "hi"}, false},
		{"flag", []string{"ask", "--json", "hi"}, true},
		{"equals true", []string{"--json=true"}, true},
		{"equals false", []string{"--json=false", "--json"}, false},
		{"false only", []string{"ask", "--json=false"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wantsJSON(tt.args); got != tt.want {
				t.Fatalf("wantsJSON(%v)=%v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestIsUsageError(t *testing.T) {
	for _, msg := range []string{
		"unknown flag: --x",
		"flag needs an argument: --x",
		"requires at least 1 arg(s), only received 0",
		"accepts 1 arg(s), received 2",
		"interactive mode is not supported with --json; provide a query",
	} {
		if !cli.IsUsageError(errors.New(msg)) {
			t.Errorf("IsUsageError(%q)=false, want true", msg)
		}
	}
	if cli.IsUsageError(errors.New("connection refused")) {
		t.Error("connection refused should not be usage error")
	}
}

func TestIsConfigError(t *testing.T) {
	for _, msg := range []string{
		"parse config /tmp/c.json: bad json",
		"read config: permission denied",
		"config path: boom",
		"invalid remote_base_url \"x\": bad",
		"temperature 3.00 out of range [0, 2]",
		"failed to save config: disk full",
	} {
		if !cli.IsConfigError(errors.New(msg)) {
			t.Errorf("IsConfigError(%q)=false, want true", msg)
		}
	}
	if cli.IsConfigError(errors.New("unknown flag: --x")) {
		t.Error("flag error should not be config error")
	}
}

func TestIsProviderError(t *testing.T) {
	cases := []error{
		llm.ErrNoLocalModel,
		llm.ErrNotBuiltWithCGO,
		errors.New("llm: unknown provider \"x\""),
		errors.New("HTTP 401 unauthorized"),
		errors.New("dial tcp: connection refused"),
		errors.New("no such host"),
		errors.New("context deadline exceeded"),
	}
	for _, err := range cases {
		if !cli.IsProviderError(err) {
			t.Errorf("IsProviderError(%q)=false, want true", err)
		}
	}
	if cli.IsProviderError(errors.New("parse config: bad")) {
		t.Error("config parse should not be provider error")
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"usage", errors.New("requires at least 1 arg(s), only received 0"), 2},
		{"config", errors.New("parse config x: bad"), 3},
		{"provider sentinel", llm.ErrNoLocalModel, 4},
		{"provider network", errors.New("dial tcp: connection refused"), 4},
		{"exit coder", exitErr{code: 7}, 7},
		{"exit coder zero", exitErr{code: 0}, 1},
		{"general", errors.New("something else"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cli.ErrorExitCode(tt.err); got != tt.want {
				t.Fatalf("ErrorExitCode(%v)=%d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestErrorHint(t *testing.T) {
	if !strings.Contains(cli.ErrorHint(llm.ErrNoLocalModel), "dmsh model download") {
		t.Error("missing model download hint for ErrNoLocalModel")
	}
	if !strings.Contains(cli.ErrorHint(errors.New("requires at least 1 arg(s)")), "--help") {
		t.Error("missing --help hint for usage error")
	}
	if !strings.Contains(cli.ErrorHint(errors.New("parse config: bad")), "config show") {
		t.Error("missing config hint")
	}
	if !strings.Contains(cli.ErrorHint(errors.New("dial tcp: connection refused")), "doctor") {
		t.Error("missing doctor hint for provider/network error")
	}
}

func TestErrorKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"usage", errors.New("unknown flag: --x"), "usage"},
		{"config", errors.New("parse config x: bad"), "config"},
		{"provider sentinel", llm.ErrNoLocalModel, "provider"},
		{"provider network", errors.New("dial tcp: connection refused"), "provider"},
		{"exec", &cli.ExitError{Code: 7}, "exec"},
		{"exec interface", exitErr{code: 3}, "exec"},
		{"general", errors.New("something else"), "general"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cli.ErrorKind(tt.err); got != tt.want {
				t.Fatalf("ErrorKind(%v)=%q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func isolateEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestRunVersionJSON(t *testing.T) {
	isolateEnv(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"version", "--json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s out=%s", code, errBuf.String(), out.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out.String())
	}
	if m["version"] == "" {
		t.Fatalf("missing version: %#v", m)
	}
}

func TestRunInteractiveJSONRejected(t *testing.T) {
	isolateEnv(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"--json"}, &out, &errBuf)
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (out=%s err=%s)", code, out.String(), errBuf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out.String())
	}
	if m["ok"] != false {
		t.Fatalf("ok=%#v, want false", m["ok"])
	}
	if _, ok := m["exit_code"]; !ok {
		t.Fatalf("expected exit_code in payload: %#v", m)
	}
	if hint, _ := m["hint"].(string); hint == "" {
		t.Fatalf("expected hint in payload: %#v", m)
	}
	if kind, _ := m["kind"].(string); kind != "usage" {
		t.Fatalf("kind=%q, want usage: %#v", kind, m)
	}
}

func TestRunBadFlagJSON(t *testing.T) {
	isolateEnv(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"--badflag", "--json"}, &out, &errBuf)
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (out=%s err=%s)", code, out.String(), errBuf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out.String())
	}
	if m["ok"] != false {
		t.Fatalf("ok=%#v, want false", m["ok"])
	}
	if kind, _ := m["kind"].(string); kind != "usage" {
		t.Fatalf("kind=%q, want usage: %#v", kind, m)
	}
}

