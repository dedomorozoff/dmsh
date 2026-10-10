package config

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

func TestValidate_DefaultsOK(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestValidate_EmptyModeNormalized(t *testing.T) {
	cfg := Default()
	cfg.Mode = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty mode should be normalized, got: %v", err)
	}
	if cfg.Mode != ModeAI {
		t.Errorf("expected ModeAI, got %q", cfg.Mode)
	}
}

func TestValidate_InvalidMode(t *testing.T) {
	cfg := Default()
	cfg.Mode = "auto"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid mode should fail validation")
	}
}

func TestValidate_NegativeThreads(t *testing.T) {
	cfg := Default()
	cfg.Threads = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("negative threads should fail validation")
	}
}

func TestValidate_BadTemperature(t *testing.T) {
	cfg := Default()
	cfg.Temperature = 3.0
	if err := cfg.Validate(); err == nil {
		t.Fatal("temperature out of range should fail validation")
	}
}

func TestValidate_BadRegex(t *testing.T) {
	cfg := Default()
	cfg.DangerPatterns = []string{`[unclosed`}
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid danger pattern regex should fail validation")
	}
}

func TestDefaultProviderIsAutoWithTools(t *testing.T) {
	cfg := Default()
	if cfg.Provider != ProviderAuto {
		t.Fatalf("default provider = %q, want %q", cfg.Provider, ProviderAuto)
	}
	if !cfg.ToolsEnabled {
		t.Fatal("tools should be enabled by default")
	}
	// Модель появляется после Validate: дефолт зависит от провайдера.
	if cfg.RemoteModel != "" {
		t.Fatalf("remote model = %q, want it to be chosen per provider", cfg.RemoteModel)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.RemoteModel != DefaultRemoteModel {
		t.Fatalf("remote model = %q, want %q for provider=auto", cfg.RemoteModel, DefaultRemoteModel)
	}
}

func TestValidate_EmptyProviderNormalized(t *testing.T) {
	cfg := Default()
	cfg.Provider = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty provider should be normalized, got: %v", err)
	}
	if cfg.Provider != ProviderAuto {
		t.Fatalf("provider = %q, want %q", cfg.Provider, ProviderAuto)
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	cfg := Default()
	cfg.Provider = "openrouter"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown provider should fail validation")
	}
}

// Ollama — полноценный провайдер, а не синоним local: у него свой адрес и
// своя модель по умолчанию.
func TestProviderOllama(t *testing.T) {
	if !ProviderOllama.Valid() {
		t.Fatal("ollama must be a valid provider")
	}
	if !ProviderOllama.Remote() {
		t.Fatal("ollama talks over HTTP, so it is remote")
	}
	if ProviderLocal.Remote() {
		t.Fatal("a local GGUF is not remote")
	}
	cfg := Default()
	cfg.Provider = ProviderOllama
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.RemoteModel != DefaultOllamaModel {
		t.Fatalf("remote model = %q, want %q", cfg.RemoteModel, DefaultOllamaModel)
	}
	if got := DefaultModelFor(ProviderOllama); got != DefaultOllamaModel {
		t.Fatalf("DefaultModelFor(ollama) = %q", got)
	}
	if got := DefaultModelFor(ProviderPollinations); got != DefaultRemoteModel {
		t.Fatalf("DefaultModelFor(pollinations) = %q", got)
	}
}

// Ключ из окружения важнее файла: так секрет можно вообще не держать на
// диске, и при этом менять его без правки конфига.
func TestAPIKeyResolution(t *testing.T) {
	cfg := Config{RemoteAPIKey: " sk-from-file "}
	t.Setenv(APIKeyEnv, "")
	if got := cfg.APIKey(); got != "sk-from-file" {
		t.Fatalf("APIKey = %q, want the file value", got)
	}
	if cfg.APIKeySource() != "config" {
		t.Fatalf("APIKeySource = %q, want config", cfg.APIKeySource())
	}
	t.Setenv(APIKeyEnv, " sk-from-env ")
	if got := cfg.APIKey(); got != "sk-from-env" {
		t.Fatalf("APIKey = %q, want the env value to win", got)
	}
	if cfg.APIKeySource() != "env" {
		t.Fatalf("APIKeySource = %q, want env", cfg.APIKeySource())
	}
	if got := cfg.MaskedAPIKey(); got != "********" || strings.Contains(got, "sk-") {
		t.Fatalf("MaskedAPIKey = %q, want a mask", got)
	}
	empty := Config{}
	t.Setenv(APIKeyEnv, "")
	if empty.MaskedAPIKey() != "" || empty.APIKeySource() != "" {
		t.Fatal("no key must not pretend to have one")
	}
}

func TestValidate_BadRemoteBaseURL(t *testing.T) {
	for _, base := range []string{"ftp://example.com", "not a url", "https://"} {
		cfg := Default()
		cfg.RemoteBaseURL = base
		if err := cfg.Validate(); err == nil {
			t.Fatalf("remote_base_url %q should fail validation", base)
		}
	}
}

func TestValidate_RemoteModelDefaulted(t *testing.T) {
	cfg := Default()
	cfg.RemoteModel = "  "
	if err := cfg.Validate(); err != nil {
		t.Fatalf("blank remote model should be normalized, got: %v", err)
	}
	if cfg.RemoteModel != DefaultRemoteModel {
		t.Fatalf("remote model = %q, want %q", cfg.RemoteModel, DefaultRemoteModel)
	}
}

func TestDefaultProxyIsAuto(t *testing.T) {
	cfg := Default()
	if cfg.ProxyMode != netproxy.ModeAuto {
		t.Fatalf("default proxy_mode = %q, want %q", cfg.ProxyMode, netproxy.ModeAuto)
	}
}

func TestValidate_EmptyProxyModeNormalized(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty proxy_mode should be normalized, got: %v", err)
	}
	if cfg.ProxyMode != netproxy.ModeAuto {
		t.Fatalf("proxy_mode = %q, want %q", cfg.ProxyMode, netproxy.ModeAuto)
	}
}

func TestValidate_InvalidProxyMode(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = "sometimes"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid proxy_mode should fail validation")
	}
}

func TestValidate_ProxyCustomRequiresHost(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = netproxy.ModeCustom
	cfg.ProxyHost = ""
	// Молча переключиться на прямое соединение нельзя: пользователь
	// выбрал прокси и ждёт, что трафик пойдёт через него.
	if err := cfg.Validate(); err == nil {
		t.Fatal("proxy_mode=custom without a host should fail validation")
	}
}

func TestValidate_ProxyBadProto(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = netproxy.ModeCustom
	cfg.ProxyHost = "127.0.0.1"
	cfg.ProxyProto = "ftp"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unsupported proxy_proto should fail validation")
	}
}

func TestValidate_ProxyBadPort(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = netproxy.ModeCustom
	cfg.ProxyHost = "127.0.0.1"
	cfg.ProxyPort = 70000
	if err := cfg.Validate(); err == nil {
		t.Fatal("out-of-range proxy_port should fail validation")
	}
}

// Старый единый адрес в конфиге должен продолжить работать: у пользователя
// на руках уже есть файлы с proxy_url.
func TestValidate_LegacyProxyURLMigratesToFields(t *testing.T) {
	cfg := Default()
	cfg.ProxyMode = netproxy.ModeCustom
	cfg.ProxyURL = "socks5://user:pass@127.0.0.1:1080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("legacy proxy_url should validate, got: %v", err)
	}
	p := cfg.Proxy()
	if p.Proto != netproxy.ProtoSOCKS5 || p.Host != "127.0.0.1" || p.Port != 1080 {
		t.Fatalf("migrated proxy = %+v, want socks5 127.0.0.1:1080", p)
	}
	if p.User != "user" || p.Password != "pass" {
		t.Fatalf("migrated credentials = %q/%q", p.User, p.Password)
	}
	if cfg.ProxyURL != "" {
		t.Fatal("after migration the config must keep the address only in fields")
	}
}

func TestValidate_ProxyBadNoProxy(t *testing.T) {
	cfg := Default()
	cfg.NoProxy = "http://localhost"
	if err := cfg.Validate(); err == nil {
		t.Fatal("no_proxy entry with a scheme should fail validation")
	}
}

func TestProxyRoundTrip(t *testing.T) {
	want := netproxy.Settings{
		Mode:    netproxy.ModeCustom,
		Proto:   netproxy.ProtoSOCKS5,
		Host:    "127.0.0.1",
		Port:    1080,
		NoProxy: "localhost, .corp.example",
	}
	cfg := Default()
	cfg.SetProxy(want)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("proxy settings should validate: %v", err)
	}
	if got := cfg.Proxy(); got != want {
		t.Fatalf("Proxy() = %+v, want %+v", got, want)
	}
}
