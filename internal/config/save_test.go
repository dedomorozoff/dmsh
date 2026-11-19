package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateConfigDir уводит ~/.config/dmsh во временный каталог: тест не
// должен трогать настоящий конфиг пользователя.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestSaveWritesPrivateConfig(t *testing.T) {
	dir := isolateConfigDir(t)
	cfg := Default()
	cfg.ProxyMode = "custom"
	cfg.ProxyHost = "proxy.local"
	cfg.ProxyUser = "user"
	cfg.ProxyPassword = "secret"

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path, err := userConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Fatalf("config path %q escaped the isolated dir %q", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"secret"`) {
		t.Fatalf("config must round-trip the proxy password, got %s", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("config perm = %o, want 600", perm)
		}
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Fatalf("config dir perm = %o, want 700", perm)
		}
	}
}

func TestSaveTightensLegacyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	isolateConfigDir(t)
	path, err := userConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config perm after Save = %o, want 600", perm)
	}
}

func TestLoadRoundTripsSavedConfig(t *testing.T) {
	isolateConfigDir(t)
	cfg := Default()
	cfg.ProxyMode = "custom"
	cfg.ProxyHost = "proxy.local"
	cfg.ProxyPort = 3128
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ProxyHost != "proxy.local" || got.ProxyPort != 3128 {
		t.Fatalf("loaded proxy = %s:%d, want proxy.local:3128", got.ProxyHost, got.ProxyPort)
	}
}
