package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func TestDocCommandOutputsReferences(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# dmsh\n\nProxy settings are here.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	cmd := NewRootCmdWithConfig(config.Default())
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"doc", "proxy", "--dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doc command: %v (%s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "README.md:") {
		t.Fatalf("doc output = %q", out.String())
	}
}

func TestDocStoreSearchAndFormatting(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	root := t.TempDir()
	guide := filepath.Join(root, "README.md")
	if err := os.WriteFile(guide, []byte("# dmsh\n\nProxy settings live in the config file.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	st, n, err := openDocStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if n == 0 {
		t.Fatal("the index must contain chunks")
	}
	hits, err := st.Search("proxy settings", 3)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search = %v, %v", hits, err)
	}
	text := formatDocHits(hits)
	if !strings.Contains(text, "README.md:") || !strings.Contains(text, "Proxy settings") {
		t.Fatalf("formatted hits = %q", text)
	}
	ctx, err := docAnswer(st, "proxy", 3)
	if err != nil || !strings.Contains(ctx, "README.md:") {
		t.Fatalf("doc context = %q, %v", ctx, err)
	}
}
