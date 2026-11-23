// fmtcheck reports Go files that are not gofmt-clean. Unlike a shell pipeline
// it behaves the same on Windows, macOS and Linux.
package main

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	bad := findUnformatted(".")
	if len(bad) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "not gofmt-clean:")
	for _, path := range bad {
		fmt.Fprintln(os.Stderr, path)
	}
	os.Exit(1)
}

func findUnformatted(root string) []string {
	var bad []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "third_party" || name == "bin" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		clean, err := format.Source(data)
		if err != nil || string(clean) != string(data) {
			bad = append(bad, filepath.ToSlash(path))
		}
		return nil
	})
	return bad
}
