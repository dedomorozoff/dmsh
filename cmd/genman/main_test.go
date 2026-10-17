package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

// canary — секрет, который не должен попасть в man-страницы ни при каких
// условиях.
const canary = "dmsh-secret-canary-4f2a"

// TestGenerateDoesNotLeakUserConfig — главная проверка: даже если у
// пользователя в конфиге лежит ключ и путь к модели, в man-страницы они не
// попадут. Раньше генератор брал NewRootCmd(), который читает файл
// пользователя, и всё это писалось в репозиторий.
func TestGenerateDoesNotLeakUserConfig(t *testing.T) {
	cfgDir := t.TempDir()
	configDir := filepath.Join(cfgDir, "dmsh")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"model_path":"` + filepath.ToSlash(filepath.Join(cfgDir, "secret-model.gguf")) + `",` +
		`"remote_api_key":"` + canary + `",` +
		`"proxy_mode":"custom","proxy_host":"` + canary + `.proxy.local",` +
		`"proxy_password":"` + canary + `","threads":3}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// config.Load читает UserConfigDir: на unix это XDG_CONFIG_HOME, на
	// windows — %AppData%. Оба перенаправляем в отдельную папку.
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AppData", cfgDir)
	t.Setenv("LOCALAPPDATA", cfgDir)

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("the test setup must be readable: %v", err)
	}
	if loaded.RemoteAPIKey != canary {
		t.Fatalf("test setup: the config was not picked up (key=%q)", loaded.RemoteAPIKey)
	}

	out := t.TempDir()
	if err := generate(out); err != nil {
		t.Fatalf("generate: %v", err)
	}
	pages, err := filepath.Glob(filepath.Join(out, "*.1"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no man pages generated: %v", err)
	}
	for _, page := range pages {
		data, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		text := string(data)
		if strings.Contains(text, canary) {
			t.Errorf("%s leaked the user config", filepath.Base(page))
		}
		if strings.Contains(text, "secret-model.gguf") {
			t.Errorf("%s leaked the user's model path", filepath.Base(page))
		}
	}
}

// docConfig не должен содержать ничего машино-зависимого: иначе man-страницы
// меняются при каждой генерации на другой машине.
func TestDocConfigIsMachineIndependent(t *testing.T) {
	cfg := docConfig()
	if cfg.Threads != 0 || cfg.GPULayers != 0 || cfg.Shell != "" {
		t.Errorf("docConfig must not freeze machine values: threads=%d gpu-layers=%d shell=%q",
			cfg.Threads, cfg.GPULayers, cfg.Shell)
	}
	if cfg.ModelPath != "" || cfg.RemoteAPIKey != "" || cfg.ProxyPassword != "" || cfg.ProxyHost != "" {
		t.Errorf("docConfig must be free of user data: %+v", cfg)
	}
}

// В man не должно быть машинных значений: «--gpu-layers=32» заморозило бы
// чужую видеокарту, а «--gpu-layers=0» читалось бы как «только CPU».
func TestGeneratedManHasNoMachineDefaults(t *testing.T) {
	out := t.TempDir()
	if err := generate(out); err != nil {
		t.Fatalf("generate: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "dmsh.1"))
	if err != nil {
		t.Fatalf("read dmsh.1: %v", err)
	}
	text := string(data)
	for _, flag := range machineFlags {
		needle := "\\fB--" + flag + "\\fP="
		i := strings.Index(text, needle)
		if i < 0 {
			t.Errorf("--%s is missing from the man page", flag)
			continue
		}
		rest := text[i+len(needle):]
		if cut := strings.IndexByte(rest, '\n'); cut >= 0 {
			rest = rest[:cut]
		}
		// Пусто и `""` — это «значения нет», всё остальное — утечка.
		if rest != "" && rest != `""` {
			t.Errorf("--%s still prints a machine default: %q", flag, rest)
		}
	}
}
