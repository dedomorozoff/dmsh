package model

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFreeSpace(t *testing.T) {
	dir := t.TempDir()
	free, ok := availableBytes(dir)
	if !ok {
		t.Skip("no free-space API on this platform")
	}
	dest := filepath.Join(dir, "model.gguf")
	if err := checkFreeSpace(dest, 1); err != nil {
		t.Fatalf("a tiny download should fit: %v", err)
	}
	if err := checkFreeSpace(dest, free+1); err == nil {
		t.Fatal("an oversized download must be refused")
	}
}

func TestDownloadCtxChecksSpaceBeforeRequest(t *testing.T) {
	if _, ok := availableBytes(t.TempDir()); !ok {
		t.Skip("no free-space API on this platform")
	}
	d := New(t.TempDir())
	// The URL would fail immediately if the download were attempted.
	info := ModelInfo{Name: "huge.gguf", URL: "http://127.0.0.1:1/huge.gguf", SizeMB: 100_000_000}
	_, err := d.DownloadCtx(context.Background(), info, nil)
	if err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("want an early free-space error, got %v", err)
	}
}
