package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWritePrivateTightensExistingFile(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "dmsh")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "config.json")
	// Так файл выглядел после старой версии dmsh.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WritePrivate(path, []byte("new")); err != nil {
		t.Fatalf("WritePrivate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("content = %q, want %q", data, "new")
	}
	if runtime.GOOS != "windows" {
		assertPerm(t, path, FilePerm)
		assertPerm(t, nested, DirPerm)
	}
}

func TestWritePrivateCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "file.txt")
	if err := WritePrivate(path, []byte("x")); err != nil {
		t.Fatalf("WritePrivate: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if runtime.GOOS != "windows" {
		assertPerm(t, path, FilePerm)
	}
}

func TestAppendPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "audit.jsonl")
	for _, line := range []string{"one\n", "two\n"} {
		f, err := AppendPrivate(path)
		if err != nil {
			t.Fatalf("AppendPrivate: %v", err)
		}
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\ntwo\n" {
		t.Fatalf("content = %q", data)
	}
	if runtime.GOOS != "windows" {
		assertPerm(t, path, FilePerm)
	}
}

// assertPerm проверяет права файла или каталога. На Windows chmod меняет
// только read-only, поэтому вызывается лишь на unix-подобных системах.
func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s: perm = %o, want %o", path, got, want)
	}
}
