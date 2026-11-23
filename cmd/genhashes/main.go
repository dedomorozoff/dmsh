// genhashes writes a SHA256SUMS file for release artifacts. It is used by
// `make checksums` on every platform so the release gate does not depend on
// sha256sum, shasum or a specific PowerShell version.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	dir := flag.String("dir", "bin", "directory with release artifacts")
	out := flag.String("out", "", "output file (default <dir>/SHA256SUMS)")
	flag.Parse()

	path := *out
	if path == "" {
		path = filepath.Join(*dir, "SHA256SUMS")
	}
	n, err := writeSums(*dir, path)
	if err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stdout, "SHA256SUMS: %d file(s) -> %s\n", n, path)
}

// writeSums hashes every dmsh* artifact in dir and writes deterministic,
// sorted SHA256SUMS lines. Returns the number of files covered.
func writeSums(dir, path string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(strings.ToLower(name), "dmsh") || name == "SHA256SUMS" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return 0, fmt.Errorf("no artifacts found in %s", dir)
	}
	sort.Strings(names)

	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		sum, err := fileHash(filepath.Join(dir, name))
		if err != nil {
			_ = f.Close()
			return 0, err
		}
		fmt.Fprintf(f, "%s  %s\n", sum, name)
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return len(names), nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genhashes:", err)
	os.Exit(1)
}
