package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "doc", "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"README.md":          "# dmsh\n\nProxy settings are configured with --proxy-mode.\n",
		"docs/guide.md":      "# Guide\n\nUse --dry-run=false to allow command execution.\n",
		"doc/archive/old.md": "obsolete proxy notes that must not be indexed\n",
		"docs/notes.txt":     "not markdown\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIndexAndSearch(t *testing.T) {
	root := testRoot(t)
	st, err := Open(filepath.Join(t.TempDir(), "docs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	n, err := st.Index(root)
	if err != nil || n == 0 {
		t.Fatalf("index = %d, %v", n, err)
	}

	hits, err := st.Search("proxy mode", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search = %v, %v", hits, err)
	}
	found := false
	for _, h := range hits {
		if h.File == "doc/archive/old.md" {
			t.Fatal("archived docs must not be indexed")
		}
		if h.File == "README.md" && h.Line >= 1 && strings.Contains(h.Text, "Proxy settings") {
			found = true
		}
	}
	if !found {
		t.Fatalf("README hit with file:line missing: %+v", hits)
	}

	hits, err = st.Search("dry-run execution", 3)
	if err != nil || len(hits) == 0 || hits[0].File != "docs/guide.md" {
		t.Fatalf("guide search = %v, %v", hits, err)
	}
	if hits, err := st.Search("   the and ", 5); err != nil || len(hits) != 0 {
		t.Fatalf("stop-word query = %v, %v", hits, err)
	}
}
