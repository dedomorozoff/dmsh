package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The integrity gate lives in the workflow, not in Go code. Guard the file so
// an unrelated edit cannot silently drop checksums, SBOM or signing again.
func TestReleaseWorkflowHasIntegrityGate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	wf := string(data)

	for _, want := range []string{
		"Generate SBOM",
		"make sbom",
		"Generate and verify SHA256SUMS",
		"sha256sum --check --status SHA256SUMS",
		"RELEASE_GPG_PRIVATE_KEY",
		"Sign and verify SHA256SUMS",
		"--detach-sign",
		"gpg --verify",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("release workflow is missing %q", want)
		}
	}

	publish := strings.Index(wf, "Create Release")
	for _, step := range []string{"Generate SBOM", "Generate and verify SHA256SUMS", "Sign and verify SHA256SUMS"} {
		at := strings.Index(wf, step)
		if at < 0 || publish < 0 || at >= publish {
			t.Errorf("step %q must run before Create Release", step)
		}
	}
}
