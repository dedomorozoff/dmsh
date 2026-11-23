package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindUnformatted(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.go")
	if err := os.WriteFile(good, []byte("package p\n\nfunc F() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package p\nfunc  F( ){}\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findUnformatted(dir)
	if len(got) != 1 || filepath.Base(got[0]) != "bad.go" {
		t.Fatalf("unformatted = %v", got)
	}
}
