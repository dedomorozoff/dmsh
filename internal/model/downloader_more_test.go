package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDownloadHelpers(t *testing.T) {
	if err := noRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("noRedirect = %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://example.test/x", nil)
	setRange(req, 0)
	if got := req.Header.Get("Range"); got != "" {
		t.Fatalf("Range for offset 0 = %q", got)
	}
	setRange(req, 42)
	if got := req.Header.Get("Range"); got != "bytes=42-" {
		t.Fatalf("Range = %q", got)
	}

	resp := &http.Response{ContentLength: 100}
	if got := downloadTotal(0, resp, false); got != 100 {
		t.Fatalf("fresh total = %d", got)
	}
	if got := downloadTotal(25, resp, true); got != 125 {
		t.Fatalf("resume total = %d", got)
	}
	resp.ContentLength = -1
	if got := downloadTotal(7, resp, true); got != 7 {
		t.Fatalf("resume without length = %d", got)
	}
	if got := downloadTotal(0, resp, false); got != 100*1024*1024 {
		t.Fatalf("unknown length fallback = %d", got)
	}

	if err := (&checksumError{want: "a", got: "b"}).Error(); !strings.Contains(err, "sha256 mismatch") {
		t.Fatalf("checksumError = %q", err)
	}
	if !isCorruptDownload(&checksumError{}) || isCorruptDownload(errors.New("boom")) {
		t.Fatal("isCorruptDownload must only match checksum failures")
	}
}

func TestExistingSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.gguf")
	if got := existingSize(path); got != 0 {
		t.Fatalf("missing file size = %d", got)
	}
	if err := os.WriteFile(path, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := existingSize(path); got != 5 {
		t.Fatalf("file size = %d", got)
	}
	if got := existingSize(dir); got != 0 {
		t.Fatalf("directory size = %d", got)
	}
}

func TestModelCatalog(t *testing.T) {
	d := New(t.TempDir())
	if got := RecommendModel(); got.Name != DefaultModelName {
		t.Fatalf("RecommendModel = %q, want %q", got.Name, DefaultModelName)
	}
	if got := DetectRAMGB(); got != 8 {
		t.Fatalf("DetectRAMGB = %d, want 8", got)
	}

	models, err := d.ListAllModels()
	if err != nil || len(models) != 0 {
		t.Fatalf("missing model dir: %v, %v", models, err)
	}
	if err := os.WriteFile(d.ModelPath(RecommendedModels[0].Name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ModelPath("notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := d.ListAllModels()
	if err != nil || len(all) != 1 || all[0].Name != RecommendedModels[0].Name {
		t.Fatalf("ListAllModels = %v, %v", all, err)
	}
	available := d.ListModels()
	if len(available) != 1 || available[0].Name != RecommendedModels[0].Name {
		t.Fatalf("ListModels = %v", available)
	}
}

func TestDownloadsWithLocalServer(t *testing.T) {
	body := []byte("gguf-content")
	mux := http.NewServeMux()
	mux.HandleFunc("/model.gguf", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	var srv *httptest.Server
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", srv.URL+"/model.gguf")
		w.WriteHeader(http.StatusFound)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	d := New(t.TempDir())
	path, err := d.DownloadURLCtx(context.Background(), srv.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("downloaded content = %q, %v", got, err)
	}
	if _, err := d.DownloadURL(srv.URL+"/model.gguf", nil); err != nil {
		t.Fatalf("existing file must be returned: %v", err)
	}

	info := ModelInfo{Name: "custom.gguf", URL: srv.URL + "/model.gguf"}
	path, err = d.Download(info, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "custom.gguf") {
		t.Fatalf("destination = %q", path)
	}
}
