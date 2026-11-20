package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func setupModelDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func modelDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	return filepath.Join(base, "dmsh", "models")
}

func writeTestModel(t *testing.T, name string, content []byte) string {
	t.Helper()
	dir := modelDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func runModelCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmdWithConfig(config.Default())
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"model"}, args...))
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestModelPruneRemovesTmpFiles(t *testing.T) {
	setupModelDir(t)
	tmpPath := writeTestModel(t, "partial.gguf.tmp", []byte("partial"))
	keepPath := writeTestModel(t, "keep.gguf", []byte("ok"))

	stdout, _, err := runModelCmd(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("gguf file should remain: %v", err)
	}
	if !strings.Contains(stdout, "partial.gguf.tmp") {
		t.Fatalf("expected pruned file in output, got: %s", stdout)
	}
}

func TestModelPruneNothing(t *testing.T) {
	setupModelDir(t)
	stdout, _, err := runModelCmd(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !strings.Contains(strings.ToLower(stdout), "nothing") {
		t.Fatalf("expected nothing-to-prune message, got: %s", stdout)
	}
}

func TestModelRemoveDeletesModelAndSidecar(t *testing.T) {
	setupModelDir(t)
	content := []byte("model-data")
	path := writeTestModel(t, "a.gguf", content)
	sidecar := path + ".sha256"
	if err := os.WriteFile(sidecar, []byte(sha256Hex(content)+"  a.gguf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runModelCmd(t, "remove", "a.gguf")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("model file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar should be removed, stat err=%v", err)
	}
	if !strings.Contains(stdout, "a.gguf") {
		t.Fatalf("expected removed name in output, got: %s", stdout)
	}
}

func TestModelRemoveMissing(t *testing.T) {
	setupModelDir(t)
	_, _, err := runModelCmd(t, "remove", "missing.gguf")
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestModelVerifyValidSidecar(t *testing.T) {
	setupModelDir(t)
	content := []byte("valid-model")
	path := writeTestModel(t, "ok.gguf", content)
	if err := os.WriteFile(path+".sha256", []byte(sha256Hex(content)+"  ok.gguf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runModelCmd(t, "verify", "ok.gguf")
	if err != nil {
		t.Fatalf("verify: %v (stdout: %s)", err, stdout)
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "ok") || !strings.Contains(stdout, "ok.gguf") {
		t.Fatalf("expected success output, got: %s", stdout)
	}
}

func TestModelVerifyMismatch(t *testing.T) {
	setupModelDir(t)
	path := writeTestModel(t, "bad.gguf", []byte("data"))
	if err := os.WriteFile(path+".sha256", []byte(strings.Repeat("a", 64)+"  bad.gguf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runModelCmd(t, "verify", "bad.gguf")
	if err == nil {
		t.Fatal("expected error for checksum mismatch")
	}
	combined := stdout + stderr
	if !strings.Contains(strings.ToLower(combined), "fail") && !strings.Contains(strings.ToLower(combined), "mismatch") {
		t.Fatalf("expected failure/mismatch in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestModelVerifyNoSidecar(t *testing.T) {
	setupModelDir(t)
	writeTestModel(t, "nosum.gguf", []byte("data"))

	stdout, stderr, err := runModelCmd(t, "verify", "nosum.gguf")
	if err == nil {
		t.Fatal("expected error when sidecar is missing")
	}
	combined := strings.ToLower(stdout + stderr)
	if !strings.Contains(combined, "sha256") && !strings.Contains(combined, "sidecar") && !strings.Contains(combined, "no checksum") {
		t.Fatalf("expected missing-sidecar message, got stdout=%q stderr=%q", stdout, stderr)
	}
}
