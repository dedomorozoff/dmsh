package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSums(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"dmsh-linux-amd64", "dmsh-windows.exe", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "SHA256SUMS")
	n, err := writeSums(dir, out)
	if err != nil || n != 2 {
		t.Fatalf("writeSums = %d, %v", n, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "dmsh-linux-amd64") ||
		!strings.Contains(lines[1], "dmsh-windows.exe") {
		t.Fatalf("SHA256SUMS = %q", data)
	}
	for _, line := range lines {
		if len(line) < 64 {
			t.Fatalf("short hash line %q", line)
		}
	}

	if _, err := writeSums(t.TempDir(), filepath.Join(dir, "empty")); err == nil {
		t.Fatal("an empty artifact directory must fail")
	}
}
