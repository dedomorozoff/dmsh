package cli

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

// Setup редактирует значение КОНФИГА, а не живую сессию: «auto» из файла не
// должно превращаться в конкретику уже при открытии окна.
func TestConfiguredProviderPrefersShellValue(t *testing.T) {
	s := &session{provider: config.ProviderOllama}
	if got := configuredProvider(config.ProviderAuto, s); got != config.ProviderAuto {
		t.Fatalf("shell value must win, got %q", got)
	}
	if got := configuredProvider("", s); got != config.ProviderOllama {
		t.Fatalf("session provider must be the fallback, got %q", got)
	}
	if got := configuredProvider("", nil); got != config.ProviderAuto {
		t.Fatalf("nothing set must mean auto, got %q", got)
	}
}

// newSetupTui — сессия на удалённом провайдере: пересборка движка в тесте
// не должна зависеть от наличия локальной модели.
func newSetupTui() tuiModel {
	rf := &rootFlags{cfg: config.Config{Provider: config.ProviderPollinations, Mode: config.ModeAI}}
	s := &session{cfg: rf.cfg, provider: config.ProviderPollinations, engine: &captureEngine{}}
	return NewTuiModel(rf, s).(tuiModel).openSetup()
}

func TestPaletteOpensSetup(t *testing.T) {
	m := newTestTui()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = mm.(tuiModel)
	mm, _ = m.handleKey(tea.KeyPressMsg{Text: "setup", Code: 's'})
	m = mm.(tuiModel)
	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.state != tuiSetup {
		t.Fatalf("state = %v, want tuiSetup", got.state)
	}
	rows := strings.Join(got.setupRows(), "\n")
	if !strings.Contains(rows, "provider") {
		t.Fatalf("setup should offer the choice of a provider:\n%s", rows)
	}
}

func TestSetupProviderCyclesWithArrows(t *testing.T) {
	m := newSetupTui()
	if m.supProvider != config.ProviderPollinations {
		t.Fatalf("initial provider = %q, want the one the session runs on", m.supProvider)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.supProvider != config.ProviderOllama {
		t.Fatalf("provider = %q, want ollama", m.supProvider)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.supProvider != config.ProviderLocal {
		t.Fatalf("provider = %q, want local", m.supProvider)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.supProvider != config.ProviderAuto {
		t.Fatalf("provider = %q, want auto (wrap)", m.supProvider)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.supProvider != config.ProviderLocal {
		t.Fatalf("provider = %q, want local", m.supProvider)
	}
	// Стрелки не трогают обычные поля: там нужен ввод.
	m.supIdx = supRowEndpoint
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.supEndpoint != "" {
		t.Fatalf("arrows must not edit the endpoint field, got %q", m.supEndpoint)
	}
}

// У Ollama свой адрес, своя модель и никакого ключа: три разных дефолта в
// одном списке легко перепутать.
func TestSetupOllamaDefaults(t *testing.T) {
	m := newSetupTui()
	m.supProvider = config.ProviderOllama
	if got := m.setupValue(supRowEndpoint); got != llm.DefaultOllamaBaseURL {
		t.Fatalf("endpoint = %q, want the ollama default", got)
	}
	if got := m.setupValue(supRowRemote); got != config.DefaultOllamaModel {
		t.Fatalf("model = %q, want the ollama default", got)
	}
	if got := m.setupRouteLine(); !strings.Contains(got, "no api key") {
		t.Fatalf("route line = %q, want a note that ollama needs no key", got)
	}
}

// Ключ не показывается в списке, но виден при вводе — иначе опечатку в нём
// не заметить.
func TestSetupAPIKeyIsMasked(t *testing.T) {
	m := newSetupTui()
	if got := m.setupValue(supRowAPIKey); !strings.Contains(got, "not set") {
		t.Fatalf("api key row = %q, want a prompt to enter one", got)
	}
	m.supIdx = supRowAPIKey
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(m, tea.KeyPressMsg{Text: "sk-typed", Code: 's'})
	if !strings.Contains(m.editLine(), "sk-typed") {
		t.Fatalf("the key must be visible while typing: %q", m.editLine())
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.supKey != "sk-typed" {
		t.Fatalf("key = %q, want the typed value", m.supKey)
	}
	rows := strings.Join(m.setupRows(), "\n")
	if strings.Contains(rows, "sk-typed") {
		t.Fatalf("the key must be masked in the list:\n%s", rows)
	}
	if !strings.Contains(rows, "********") {
		t.Fatalf("the list should show a mask:\n%s", rows)
	}
}

// Показанная модель — это то, что уйдёт в запрос: пустое поле значит
// дефолт выбранного провайдера, а не пустую модель.
func TestSetupRemoteModelIsResolved(t *testing.T) {
	m := newSetupTui()
	m.supProvider = config.ProviderOllama
	m.supRemote = ""
	if got := m.remoteModel(); got != config.DefaultOllamaModel {
		t.Fatalf("remoteModel = %q, want the ollama default", got)
	}
	m.supProvider = config.ProviderPollinations
	if got := m.remoteModel(); got != config.DefaultRemoteModel {
		t.Fatalf("remoteModel = %q, want the pollinations default", got)
	}
}

// Пункт setup описывает, что произойдёт с ближайшим запросом: «auto»
// иначе выглядит как «что-то».
func TestSetupShowsRouteLine(t *testing.T) {
	m := newSetupTui()
	m.supProvider = config.ProviderLocal
	if got := m.setupRouteLine(); !strings.Contains(got, "local GGUF") {
		t.Fatalf("route line = %q, want a local model", got)
	}
	m.supProvider = config.ProviderPollinations
	m.supRemote = "openai"
	m.supEndpoint = "https://example.test/v1"
	if got := m.setupRouteLine(); !strings.Contains(got, "https://example.test/v1") {
		t.Fatalf("route line = %q, want the endpoint", got)
	}
	m.supProvider = config.ProviderAuto
	if got := m.setupRouteLine(); !strings.Contains(got, "otherwise") {
		t.Fatalf("route line = %q, want the auto rule", got)
	}
}

// Применение пересобирает движок и пишет провайдера в конфиг.
func TestSetupApplyRebuildsEngine(t *testing.T) {
	isolateModelDir(t)
	m := newSetupTui()
	oldEngine := m.s.engine
	m.supProvider = config.ProviderPollinations
	m.supRemote = "gemini-search"
	m.supIdx = supRowApply
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.supStatusErr {
		t.Fatalf("apply failed: %s", got.supStatus)
	}
	if got.s.engine == oldEngine {
		t.Fatal("engine must be rebuilt after switching the connection")
	}
	if got.s.cfg.RemoteModel != "gemini-search" {
		t.Fatalf("remote model = %q, want gemini-search", got.s.cfg.RemoteModel)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RemoteModel != "gemini-search" {
		t.Fatalf("persisted remote model = %q", cfg.RemoteModel)
	}
	if !strings.Contains(got.supStatus, string(config.ProviderPollinations)) {
		t.Fatalf("status should name the connection: %q", got.supStatus)
	}
}

// Провайдер local без модели на диске обязан падать с понятной ошибкой, а
// не оставлять сессию без движка.
func TestSetupRejectsLocalWithoutModel(t *testing.T) {
	isolateModelDir(t)
	m := newSetupTui()
	oldEngine := m.s.engine
	m.supProvider = config.ProviderLocal
	m.supLocal = ""
	m.supIdx = supRowApply
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if !got.supStatusErr {
		t.Fatal("provider=local without a GGUF must be reported as an error")
	}
	if got.s.engine != oldEngine {
		t.Fatal("a failed switch must keep the working engine")
	}
	if got.s.provider != config.ProviderPollinations {
		t.Fatalf("session provider = %q, want the previous one", got.s.provider)
	}
}

// Битый endpoint обнаруживается до пересборки движка.
func TestSetupRejectsBadEndpoint(t *testing.T) {
	isolateModelDir(t)
	m := newSetupTui()
	oldEngine := m.s.engine
	m.supEndpoint = "ftp://example.test"
	m.supIdx = supRowApply
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if !got.supStatusErr {
		t.Fatal("an endpoint that is not http(s) must be reported")
	}
	if got.s.engine != oldEngine {
		t.Fatal("a rejected endpoint must not touch the engine")
	}
}

func TestSetupEditEscapeKeepsOldValue(t *testing.T) {
	m := newSetupTui()
	m.supIdx = supRowRemote
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.supEditing {
		t.Fatal("Escape should leave edit mode")
	}
	if m.state != tuiSetup {
		t.Fatal("Escape in edit mode must not close the whole window")
	}
	if m.supRemote == "x" {
		t.Fatal("the edited value must be discarded")
	}
}

func TestSetupCloseWithoutApplyKeepsSession(t *testing.T) {
	isolateModelDir(t)
	m := newSetupTui()
	m.supProvider = config.ProviderLocal
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %v, want tuiIdle", m.state)
	}
	if m.s.cfg.Provider != config.ProviderPollinations {
		t.Fatal("unapplied edits must not reach the session")
	}
	if !strings.Contains(m.content, "without applying") {
		t.Fatalf("closing with unapplied edits should warn the user, got:\n%s", m.content)
	}
}

func TestSetupRowsAreSingleLines(t *testing.T) {
	m := newSetupTui()
	m.width = 100
	for i, row := range m.setupRows() {
		if strings.Contains(row, "\n") {
			t.Fatalf("row %d spans several lines: %q", i, row)
		}
	}
	if got, want := m.setupModal().cursorY, supRowProvider; got != want {
		t.Fatalf("modal cursor row = %d, want %d", got, want)
	}
	m.supEditing = true
	m.input = "gemini-search"
	m.cursorPos = runeSliceLen(m.input)
	box := m.setupModal()
	if got, want := box.cursorY, len(box.rows)-1; got != want {
		t.Fatalf("modal cursor row while editing = %d, want %d (the input line)", got, want)
	}
	if box.cursorX < len("remote model> ") {
		t.Fatalf("modal cursor column = %d, want at least the prompt width", box.cursorX)
	}
}

// База для записи — конфиг сессии: значения из флагов запуска не должны
// молча теряться после переключения провайдера.
func TestSetupKeepsRuntimeSettings(t *testing.T) {
	isolateModelDir(t)
	m := newSetupTui()
	m.rf.cfg.Threads = 3
	m.rf.cfg.MaxTokens = 256
	m.rf.cfg.Temperature = 0.1
	m.rf.cfg.TopP = 0.8
	m.supProvider = config.ProviderPollinations
	m.supIdx = supRowApply
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.supStatusErr {
		t.Fatalf("apply failed: %s", got.supStatus)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Threads != 3 || cfg.MaxTokens != 256 {
		t.Fatalf("runtime settings were dropped: threads=%d max_tokens=%d", cfg.Threads, cfg.MaxTokens)
	}
}

func TestShowSetupPrintsConnection(t *testing.T) {
	var out strings.Builder
	showSetup(&out, &session{
		cfg:      config.Config{Provider: config.ProviderPollinations, RemoteModel: "openai", RemoteBaseURL: "https://example.test/v1"},
		provider: config.ProviderPollinations,
	})
	text := out.String()
	for _, want := range []string{"pollinations", "openai", "https://example.test/v1", "Ctrl+P"} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup output should mention %q:\n%s", want, text)
		}
	}
}
