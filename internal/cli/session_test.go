package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

// isolateModelDir уводит каталог моделей в отдельную временную папку,
// чтобы тесты не зависели от моделей, скачанных на машине.
func isolateModelDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestResolveProviderPollinationsIgnoresLocalModels(t *testing.T) {
	isolateModelDir(t)
	t.Setenv(config.APIKeyEnv, "")
	provider, notice, err := resolveProvider(config.Config{Provider: config.ProviderPollinations})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if provider != config.ProviderPollinations {
		t.Fatalf("provider = %q, want pollinations", provider)
	}
	// Явный провайдер не откатывается на локальную модель, но отсутствие
	// ключа — это ровно то, о чём пользователь должен узнать заранее.
	if !strings.Contains(notice, config.APIKeyEnv) {
		t.Fatalf("notice must mention the missing key, got %q", notice)
	}
	t.Setenv(config.APIKeyEnv, "sk-test")
	if _, notice, _ = resolveProvider(config.Config{Provider: config.ProviderPollinations}); notice != "" {
		t.Fatalf("a configured key must not produce a notice, got %q", notice)
	}
}

func TestResolveProviderAutoFallsBackWhenNoModel(t *testing.T) {
	isolateModelDir(t)
	provider, notice, err := resolveProvider(config.Config{Provider: config.ProviderAuto})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if provider != config.ProviderPollinations {
		t.Fatalf("provider = %q, want pollinations when no gguf exists", provider)
	}
	if !strings.Contains(notice, "Pollinations") {
		t.Fatalf("notice must explain the fallback, got %q", notice)
	}
}

func TestResolveProviderAutoPrefersLocalModel(t *testing.T) {
	isolateModelDir(t)
	path := writeFakeModel(t, "tiny.gguf")

	provider, notice, err := resolveProvider(config.Config{Provider: config.ProviderAuto})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if provider != config.ProviderLocal {
		t.Fatalf("provider = %q, want local when a gguf exists", provider)
	}
	if notice != "" {
		t.Fatalf("local provider must not produce a notice: %q", notice)
	}

	provider, _, err = resolveProvider(config.Config{Provider: "", ModelPath: path})
	if err != nil {
		t.Fatalf("resolveProvider(explicit path): %v", err)
	}
	if provider != config.ProviderLocal {
		t.Fatalf("provider = %q, want local for an explicit model path", provider)
	}
}

// Единый порядок выбора провайдера: сессия, её конфиг, конфиг оболочки,
// и только потом auto. Один хелпер вместо четырёх разных цепочек.
func TestCurrentProviderChain(t *testing.T) {
	s := &session{cfg: config.Config{Provider: config.ProviderOllama}, provider: config.ProviderPollinations}
	if got := currentProvider(s, config.ProviderLocal); got != config.ProviderPollinations {
		t.Fatalf("session provider must win, got %q", got)
	}
	s.provider = ""
	if got := currentProvider(s, config.ProviderLocal); got != config.ProviderOllama {
		t.Fatalf("session config must be next, got %q", got)
	}
	s.cfg.Provider = ""
	if got := currentProvider(s, config.ProviderLocal); got != config.ProviderLocal {
		t.Fatalf("shell config must be next, got %q", got)
	}
	if got := currentProvider(nil, ""); got != config.ProviderAuto {
		t.Fatalf("no candidates must mean auto, got %q", got)
	}
}

func TestResolveProviderLocalWithoutModelFails(t *testing.T) {
	isolateModelDir(t)
	_, _, err := resolveProvider(config.Config{Provider: config.ProviderLocal})
	if err == nil {
		t.Fatal("provider=local without a model must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "dmsh model download") || !strings.Contains(msg, "provider auto") {
		t.Fatalf("error should point to both ways out, got %q", msg)
	}
}

// Ollama выбирается явно и остаётся им: сервер локальный, ключ не нужен,
// а адрес у него свой.
func TestResolveProviderOllama(t *testing.T) {
	isolateModelDir(t)
	provider, notice, err := resolveProvider(config.Config{Provider: config.ProviderOllama})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if provider != config.ProviderOllama {
		t.Fatalf("provider = %q, want ollama", provider)
	}
	if !strings.Contains(notice, llm.DefaultOllamaBaseURL) {
		t.Fatalf("notice should name the address used, got %q", notice)
	}
}

func TestResolveProviderUnknown(t *testing.T) {
	isolateModelDir(t)
	if _, _, err := resolveProvider(config.Config{Provider: config.Provider("llamacpp")}); err == nil {
		t.Fatal("unknown provider must fail")
	}
}

func TestToLLMProvider(t *testing.T) {
	cases := map[config.Provider]llm.Provider{
		config.ProviderLocal:        llm.ProviderLocal,
		config.ProviderPollinations: llm.ProviderPollinations,
		config.ProviderOllama:       llm.ProviderOllama,
		config.ProviderAuto:         llm.ProviderAuto,
		config.Provider("weird"):    llm.ProviderAuto,
	}
	for in, want := range cases {
		if got := toLLMProvider(in); got != want {
			t.Fatalf("toLLMProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

// Модель по умолчанию принадлежит провайдеру: у Ollama и Pollinations они
// разные, и флаг --provider не должен тащить чужую.
func TestNormalizeForProviderPicksOwnDefault(t *testing.T) {
	isolateModelDir(t)
	cfg, err := normalizeForProvider(config.Config{Provider: config.ProviderOllama, RemoteModel: ""}, config.ProviderOllama)
	if err != nil {
		t.Fatalf("normalizeForProvider: %v", err)
	}
	if cfg.RemoteModel != config.DefaultOllamaModel {
		t.Fatalf("RemoteModel = %q, want %q", cfg.RemoteModel, config.DefaultOllamaModel)
	}
	if cfg.ModelPath != "" {
		t.Fatalf("remote provider must not keep a local model path, got %q", cfg.ModelPath)
	}
	kept, err := normalizeForProvider(config.Config{Provider: config.ProviderOllama, RemoteModel: "llama3.2"}, config.ProviderOllama)
	if err != nil {
		t.Fatalf("normalizeForProvider: %v", err)
	}
	if kept.RemoteModel != "llama3.2" {
		t.Fatalf("RemoteModel = %q, want the explicit choice", kept.RemoteModel)
	}
}

func TestNewSessionUsesRemoteProviderWithoutModel(t *testing.T) {
	isolateModelDir(t)
	s, err := newSession(config.Config{
		Provider:     config.ProviderPollinations,
		RemoteModel:  "openai",
		Shell:        "powershell",
		ToolsEnabled: true,
		Mode:         config.ModeAI,
	})
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	defer s.close()

	if s.provider != config.ProviderPollinations {
		t.Fatalf("session provider = %q", s.provider)
	}
	if !llm.SupportsTools(s.engine) {
		t.Fatal("pollinations engine must support tools")
	}
	if s.cfg.ModelPath != "" {
		t.Fatalf("remote session must not keep a local model path, got %q", s.cfg.ModelPath)
	}
}

func TestNewSessionLocalWithoutModelFails(t *testing.T) {
	isolateModelDir(t)
	if _, err := newSession(config.Config{Provider: config.ProviderLocal}); err == nil {
		t.Fatal("local provider without a model must fail")
	}
}

func TestShowModelRemoteProvider(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	s := &session{
		cfg:      config.Config{Provider: config.ProviderPollinations, RemoteModel: "mistral"},
		provider: config.ProviderPollinations,
	}
	var out strings.Builder
	showModel(&out, s)
	text := out.String()
	for _, want := range []string{"pollinations", "mistral", llm.DefaultPollinationsBaseURL, "no api key"} {
		if !strings.Contains(text, want) {
			t.Fatalf("showModel output should mention %q:\n%s", want, text)
		}
	}

	// Ключ виден только откуда он, но никогда сам: строка с секретом в вывод
	// сессии попадать не должна.
	t.Setenv(config.APIKeyEnv, "sk-secret-value")
	s.cfg.RemoteAPIKey = "sk-file-value"
	out.Reset()
	showModel(&out, s)
	text = out.String()
	for _, secret := range []string{"sk-secret-value", "sk-file-value"} {
		if strings.Contains(text, secret) {
			t.Fatalf("api key leaked into /model output: %q", secret)
		}
	}
	if !strings.Contains(text, config.APIKeyEnv) {
		t.Fatalf("output should name where the key comes from:\n%s", text)
	}
}

func TestShowModelLocalWithAutoHint(t *testing.T) {
	s := &session{
		cfg:      config.Config{ModelPath: filepath.Join("models", "tiny.gguf")},
		provider: config.ProviderAuto,
	}
	var out strings.Builder
	showModel(&out, s)
	// auto с локальной моделью — это локальный провайдер, а подсказка
	// предлагает альтернативу, а не описывает уже выбранное.
	if !strings.Contains(out.String(), filepath.Join("models", "tiny.gguf")) {
		t.Fatalf("auto with a gguf must show the local model:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "dmsh config set provider") {
		t.Fatalf("auto should hint at the alternative provider:\n%s", out.String())
	}
}

func TestShowModelOllama(t *testing.T) {
	s := &session{
		cfg:      config.Config{Provider: config.ProviderOllama, RemoteModel: "llama3.2"},
		provider: config.ProviderOllama,
	}
	var out strings.Builder
	showModel(&out, s)
	text := out.String()
	for _, want := range []string{"ollama", "llama3.2", llm.DefaultOllamaBaseURL} {
		if !strings.Contains(text, want) {
			t.Fatalf("showModel output should mention %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "no api key") {
		t.Fatalf("ollama needs no key, so the line about it is noise:\n%s", text)
	}
}

func TestStatuslineShowsRemoteModel(t *testing.T) {
	m := NewTuiModel(
		&rootFlags{cfg: config.Config{Provider: config.ProviderPollinations}},
		&session{
			cfg:      config.Config{Provider: config.ProviderPollinations, RemoteModel: "mistral"},
			provider: config.ProviderPollinations,
		},
	).(tuiModel)
	if got := ansi.Strip(m.statusline()); !strings.Contains(got, "model:pollinations:mistral") {
		t.Fatalf("statusline = %q", got)
	}
}

func writeFakeModel(t *testing.T, name string) string {
	t.Helper()
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	dir := filepath.Join(base, "dmsh", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("GGUF"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
