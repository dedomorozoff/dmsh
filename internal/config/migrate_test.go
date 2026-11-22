package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrateV0ToV1(t *testing.T) {
	cfg := Default()
	raw := []byte(`{"proxy_url":"http://user:secret@proxy.local:3128","dry_run":false}`)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := migrate(&cfg, raw); err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("version = %d, want %d", cfg.ConfigVersion, CurrentConfigVersion)
	}
	if cfg.ProxyHost != "proxy.local" || cfg.ProxyPort != 3128 || cfg.ProxyUser != "user" || cfg.ProxyPassword != "secret" {
		t.Fatalf("legacy proxy was not expanded: %+v", cfg)
	}
	if cfg.ProxyURL != "" {
		t.Fatal("legacy proxy_url must be cleared after migration")
	}
	if cfg.DryRun {
		t.Fatal("an explicit dry_run=false from v0 must survive migration")
	}
}

func TestMigrateRejectsFutureVersion(t *testing.T) {
	cfg := Default()
	err := migrate(&cfg, []byte(`{"config_version":99}`))
	if err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("want a future-version error, got %v", err)
	}
}

func TestLoadMigratesLegacyProxyURL(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	path := filepath.Join(dir, "dmsh", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"provider":"pollinations","proxy_url":"http://proxy.local:3128","dry_run":false}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigVersion != CurrentConfigVersion || cfg.ProxyHost != "proxy.local" || cfg.DryRun {
		t.Fatalf("legacy config was not migrated: %+v", cfg)
	}
}
