package model

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256SidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "m.gguf")
	content := []byte("hello model")
	if err := os.WriteFile(modelPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if err := os.WriteFile(SHA256SidecarPath(modelPath), []byte(want+"  m.gguf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSHA256Sidecar(SHA256SidecarPath(modelPath))
	if err != nil {
		t.Fatalf("ReadSHA256Sidecar: %v", err)
	}
	if got != want {
		t.Fatalf("sum = %q, want %q", got, want)
	}
	checked, err := VerifySHA256Sidecar(modelPath)
	if err != nil || !checked {
		t.Fatalf("VerifySHA256Sidecar = (%v, %v), want (true, nil)", checked, err)
	}
}

func TestVerifySHA256SidecarMismatch(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(modelPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SHA256SidecarPath(modelPath), []byte(stringsRepeat("a", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checked, err := VerifySHA256Sidecar(modelPath)
	if !checked || err == nil {
		t.Fatalf("expected mismatch, got checked=%v err=%v", checked, err)
	}
}

func TestVerifySHA256SidecarMissing(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(modelPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	checked, err := VerifySHA256Sidecar(modelPath)
	if checked || err != nil {
		t.Fatalf("missing sidecar: got checked=%v err=%v, want (false, nil)", checked, err)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
