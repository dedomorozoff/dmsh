package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func runComplete(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"__complete"}, args...))
	_ = cmd.Execute()
	return out.String()
}

func completionHas(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		// __complete lines can be "value\tDescription"
		field := strings.SplitN(line, "\t", 2)[0]
		if field == want {
			return true
		}
	}
	return false
}

func TestCompletionModelUseSuggestsDownloadedModels(t *testing.T) {
	setupModelDir(t)
	writeTestModel(t, "alpha.gguf", []byte("a"))
	out := runComplete(t, "model", "use", "")
	if !completionHas(out, "alpha.gguf") {
		t.Fatalf("expected alpha.gguf in completions, got:\n%s", out)
	}
}

func TestCompletionModelPathSuggestsDownloadedModels(t *testing.T) {
	setupModelDir(t)
	writeTestModel(t, "beta.gguf", []byte("b"))
	out := runComplete(t, "model", "path", "")
	if !completionHas(out, "beta.gguf") {
		t.Fatalf("expected beta.gguf in completions, got:\n%s", out)
	}
}

func TestCompletionConfigSetSuggestsKeys(t *testing.T) {
	out := runComplete(t, "config", "set", "")
	for _, want := range []string{"provider", "mode", "dry_run"} {
		if !completionHas(out, want) {
			t.Fatalf("expected %q in config set key completions, got:\n%s", want, out)
		}
	}
}

func TestCompletionConfigSetProviderSuggestsValues(t *testing.T) {
	out := runComplete(t, "config", "set", "provider", "")
	for _, want := range []string{"auto", "local", "ollama", "pollinations"} {
		if !completionHas(out, want) {
			t.Fatalf("expected %q in provider value completions, got:\n%s", want, out)
		}
	}
}

func TestCompletionConfigSetModeSuggestsValues(t *testing.T) {
	out := runComplete(t, "config", "set", "mode", "")
	for _, want := range []string{"ai", "help", "shell"} {
		if !completionHas(out, want) {
			t.Fatalf("expected %q in mode value completions, got:\n%s", want, out)
		}
	}
}
