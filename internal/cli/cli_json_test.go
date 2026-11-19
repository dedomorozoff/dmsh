package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func TestVersionJSON(t *testing.T) {
	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"version", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, errBuf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if m["version"] == "" || m["platform"] == "" || m["go"] == "" {
		t.Fatalf("unexpected version payload: %#v", m)
	}
}

func TestInteractiveJSONRejected(t *testing.T) {
	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for interactive mode with --json")
	}
	if !strings.Contains(err.Error(), "interactive mode is not supported with --json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigShowJSONMasksSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	cfg := config.Default()
	cfg.RemoteAPIKey = "sk-secret-value"
	cfg.ProxyPassword = "proxy-secret"
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"config", "show", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, errBuf.String())
	}
	text := out.String()
	if strings.Contains(text, "sk-secret-value") || strings.Contains(text, "proxy-secret") {
		t.Fatalf("secrets leaked in JSON output: %s", text)
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if m["remote_api_key_set"] != true {
		t.Fatalf("remote_api_key_set = %#v, want true", m["remote_api_key_set"])
	}
	if m["proxy_password_set"] != true {
		t.Fatalf("proxy_password_set = %#v, want true", m["proxy_password_set"])
	}
}

func TestAskJSONMissingArgIsUsageError(t *testing.T) {
	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"ask", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "requires at least") {
		t.Fatalf("expected usage error, got: %v", err)
	}
}

func TestDoctorJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, errBuf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	checks, ok := m["checks"].([]any)
	if !ok || len(checks) == 0 {
		t.Fatalf("expected checks array, got %#v", m["checks"])
	}
}
