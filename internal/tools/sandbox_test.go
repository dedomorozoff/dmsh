package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadFileOutsideRootsIsBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry().Restrict(root)
	res := decodeResult(t, r.Call(context.Background(), NameReadFile, mustJSON(t, ReadFileArgs{Path: secret})))
	if res.OK {
		t.Fatalf("read outside sandbox must fail: %+v", res)
	}
	if !strings.Contains(res.Error, "outside the allowed roots") {
		t.Fatalf("unexpected error: %+v", res)
	}
}

func TestListDirOutsideRootsIsBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	r := NewRegistry().Restrict(root)
	res := decodeResult(t, r.Call(context.Background(), NameListDir, mustJSON(t, ListDirArgs{Path: outside})))
	if res.OK {
		t.Fatalf("list outside sandbox must fail: %+v", res)
	}
	if !strings.Contains(res.Error, "outside the allowed roots") {
		t.Fatalf("unexpected error: %+v", res)
	}
}

func TestReadFileInsideRootIsAllowed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry().Restrict(root)
	res := decodeResult(t, r.Call(context.Background(), NameReadFile, mustJSON(t, ReadFileArgs{Path: path})))
	if !res.OK || !strings.Contains(res.Content, "ok") {
		t.Fatalf("read inside sandbox must work: %+v", res)
	}
}

func TestUnrestrictedRegistryAllowsAnyReadablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry() // без Restrict — совместимость с прямыми вызовами
	res := decodeResult(t, r.Call(context.Background(), NameReadFile, mustJSON(t, ReadFileArgs{Path: path})))
	if !res.OK {
		t.Fatalf("unrestricted registry must read: %+v", res)
	}
}

func TestSymlinkEscapeIsBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require extra privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "leak.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := NewRegistry().Restrict(root)
	res := decodeResult(t, r.Call(context.Background(), NameReadFile, mustJSON(t, ReadFileArgs{Path: link})))
	if res.OK {
		t.Fatalf("symlink escape must fail: %+v", res)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
