package model

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

func TestNewDownloaderHasNoRedirect(t *testing.T) {
	d := New(t.TempDir())
	if d.client.CheckRedirect == nil {
		t.Fatal("downloads must not follow redirects: the 302 is handled by downloadFile")
	}
}

// TestSetProxyRoutesDownloadThroughProxy проверяет главное: скачивание GGUF
// идёт через настроенный прокси, а не мимо него.
func TestSetProxyRoutesDownloadThroughProxy(t *testing.T) {
	var hits int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Length", "4")
		_, _ = w.Write([]byte("gguf"))
	}))
	defer proxy.Close()

	if err := SetProxy(netproxy.Settings{Mode: netproxy.ModeCustom, URL: proxy.URL}); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	t.Cleanup(func() {
		downloadMu.Lock()
		downloadHTTP = nil
		downloadMu.Unlock()
	})

	d := New(t.TempDir())
	path, err := d.DownloadURLCtx(t.Context(), "http://models.invalid/tiny.gguf", nil)
	if err != nil {
		t.Fatalf("DownloadURLCtx: %v", err)
	}
	if hits != 1 {
		t.Fatalf("proxy hits = %d, want 1", hits)
	}
	if !d.Exists("tiny.gguf") {
		t.Fatalf("file was not saved to %s", path)
	}
}

func TestSetProxyRejectsBadURL(t *testing.T) {
	if err := SetProxy(netproxy.Settings{Mode: netproxy.ModeCustom, Proto: "ftp", Host: "127.0.0.1", Port: 21}); err == nil {
		t.Fatal("unsupported proxy protocol should be rejected")
	}
	if err := SetProxy(netproxy.Settings{Mode: netproxy.ModeCustom}); err == nil {
		t.Fatal("proxy_mode=custom without a host should be rejected")
	}
}