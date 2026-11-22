package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestPluginToolName(t *testing.T) {
	cases := map[string]string{
		"git":       "plugin_git",
		"dmsh-git":  "plugin_git",
		" My-Tool ": "plugin_my-tool",
		"bad name":  "",
		"":          "",
	}
	for in, want := range cases {
		if got := PluginToolName(in); got != want {
			t.Errorf("PluginToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"one two", []string{"one", "two"}},
		{`a "b c" d`, []string{"a", "b c", "d"}},
		{"'a b' c", []string{"a b", "c"}},
		{`a\ b`, []string{"a b"}},
	}
	for _, tc := range cases {
		got, err := SplitArgs(tc.in)
		if err != nil {
			t.Fatalf("SplitArgs(%q): %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := SplitArgs(`"unfinished`); err == nil {
		t.Fatal("an unterminated quote must be rejected")
	}
}

func TestDiscoverPluginsUsesAllowlist(t *testing.T) {
	dir := t.TempDir()
	name := "dmsh-demo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dmsh-other.exe"), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}

	found := DiscoverPlugins(dir, []string{"dmsh-demo", "other", "demo", "bad name"})
	names := make([]string, 0, len(found))
	for _, p := range found {
		names = append(names, p.Name)
	}
	want := []string{"demo", "other"}
	if runtime.GOOS != "windows" {
		want = []string{"demo"} // no .exe on Unix
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("plugins = %v, want %v", names, want)
	}
	if got := DiscoverPlugins(dir, nil); len(got) != 0 {
		t.Fatalf("an empty allowlist must expose nothing, got %v", got)
	}
}
