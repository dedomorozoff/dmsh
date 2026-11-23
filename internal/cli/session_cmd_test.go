package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupSessionDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "dmsh", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRenameNamedSession(t *testing.T) {
	dir := setupSessionDir(t)
	old := sessionFileNamed("work")
	if err := os.WriteFile(old, []byte(`[{"user":"hi"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	name, err := renameNamedSession("work", "my work")
	if err != nil || name != "my_work" {
		t.Fatalf("rename = %q, %v", name, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old session file must be gone")
	}
	if _, err := os.Stat(sessionFileNamed("my_work")); err != nil {
		t.Fatalf("new session file missing: %v", err)
	}
	names, err := listNamedSessions()
	if err != nil || len(names) != 1 || names[0] != "my_work" {
		t.Fatalf("sessions = %v, %v", names, err)
	}
	_ = dir
}

func TestRenameDefaultSession(t *testing.T) {
	setupSessionDir(t)
	def := sessionFileNamed("")
	if err := os.WriteFile(def, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renameNamedSession("", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionFileNamed("first")); err != nil {
		t.Fatalf("named session missing: %v", err)
	}
}

func TestRenameNamedSessionRefusesBadTargets(t *testing.T) {
	setupSessionDir(t)
	src := sessionFileNamed("a")
	if err := os.WriteFile(src, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := sessionFileNamed("b")
	if err := os.WriteFile(dst, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		old, new, want string
	}{
		{"a", "b", "already exists"},
		{"missing", "c", "not found"},
		{"a", "a", "already named"},
		{"a", "...", "invalid"},
	} {
		_, err := renameNamedSession(tc.old, tc.new)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("rename(%q, %q) = %v, want %q", tc.old, tc.new, err, tc.want)
		}
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("a rejected rename must leave the source intact")
	}
}
