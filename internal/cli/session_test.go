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
	provider, notice, err := resolveProvider(config.Config{Provider: config.ProviderPollinations})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if provider != config.ProviderPollinations {
		t.Fatalf("provider = %q, want pollinations", provider)
	}
	if notice != "" {
		t.Fatalf("explicit provider must not produce a notice: %q", notice)
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

func TestResolveProviderUnknown(t *testing.T) {
	isolateModelDir(t)
	if _, _, err := resolveProvider(config.Config{Provider: config.Provider("ollama")}); err == nil {
		t.Fatal("unknown provider must fail")
	}
}

func TestToLLMProvider(t *testing.T) {
	cases := map[config.Provider]llm.Provider{
		config.ProviderLocal:        llm.ProviderLocal,
		config.ProviderPollinations: llm.ProviderPollinations,
		config.ProviderAuto:         llm.ProviderAuto,
		config.Provider("weird"):    llm.ProviderAuto,
	}
	for in, want := range cases {
		if got := toLLMProvider(in); got != want {
			t.Fatalf("toLLMProvider(%q) = %q, want %q", in, got, want)
		}
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
	s := &session{
		cfg:      config.Config{Provider: config.ProviderPollinations, RemoteModel: "mistral"},
		provider: config.ProviderPollinations,
	}
	var out strings.Builder
	showModel(&out, s)
	text := out.String()
	for _, want := range []string{"pollinations", "mistral", llm.DefaultPollinationsBaseURL, "anonymous"} {
		if !strings.Contains(text, want) {
			t.Fatalf("showModel output should mention %q:\n%s", want, text)
		}
	}
}

func TestShowModelLocalWithAutoHint(t *testing.T) {
	s := &session{
		cfg:      config.Config{ModelPath: filepath.Join("models", "tiny.gguf")},
		provider: config.ProviderAuto,
	}
	var out strings.Builder
	showModel(&out, s)
	if !strings.Contains(out.String(), "dmsh config set provider pollinations") {
		t.Fatalf("auto should hint at the remote provider:\n%s", out.String())
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
