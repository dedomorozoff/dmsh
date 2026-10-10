package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

// newRemoteMenuTui — сессия на удалённом провайдере: у неё меню моделей
// показывает каталог, а не файлы на диске.
func newRemoteMenuTui(t *testing.T, srv *httptest.Server) tuiModel {
	t.Helper()
	rf := &rootFlags{cfg: config.Config{Provider: config.ProviderOllama, RemoteBaseURL: srv.URL, Mode: config.ModeAI}}
	s := &session{
		cfg:      config.Config{Provider: config.ProviderOllama, RemoteBaseURL: srv.URL, RemoteModel: "qwen3:8b"},
		provider: config.ProviderOllama,
		engine:   &captureEngine{},
	}
	return NewTuiModel(rf, s).(tuiModel)
}

// Меню моделей у удалённого провайдера показывает его каталог, а не локальные
// gguf: список приходит по сети и может быть длинным.
func TestRemoteModelMenuLoadsCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:8b"},{"id":"qwen3:4b"},{"id":"deepcoder:1.5b"}]}`))
	}))
	defer srv.Close()

	m := newRemoteMenuTui(t, srv)
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.state != tuiModelMenu {
		t.Fatalf("state = %d, want tuiModelMenu", m.state)
	}
	// Пока список не пришёл, меню показывает загрузку, а не пустоту.
	if !m.modelBusy || !strings.Contains(m.modelStatus, "ollama") {
		t.Fatalf("menu should show the fetch in progress: status=%q busy=%v", m.modelStatus, m.modelBusy)
	}
	if len(m.modelItems) != 0 {
		t.Fatalf("items before the fetch = %+v, want none", m.modelItems)
	}

	mm, _ := m.Update(remoteModelsMsg{models: []llm.RemoteModel{
		{ID: "deepcoder:1.5b"}, {ID: "qwen3:4b"}, {ID: "qwen3:8b"},
	}})
	got := mm.(tuiModel)
	if got.modelBusy {
		t.Fatal("the menu must stop showing progress after the catalog arrives")
	}
	if len(got.modelItems) != 3 {
		t.Fatalf("items = %+v, want three", got.modelItems)
	}
	for i, it := range got.modelItems {
		if !it.remote {
			t.Fatalf("item %d must be marked as a provider model: %+v", i, it)
		}
	}
	// Текущая модель помечена, и выделение стоит на ней.
	if !got.modelItems[2].isCurrent || got.modelIdx != 2 {
		t.Fatalf("selection = %d, want the current model at %d", got.modelIdx, 2)
	}
	if !strings.Contains(got.render(), "qwen3:4b") {
		t.Fatalf("the catalog should be on screen:\n%s", got.render())
	}
}

// Ошибка загрузки каталога остаётся на экране: «сервер не запущен» и
// «нужен ключ» пользователю нужны, а не теряются в пустом списке.
func TestRemoteModelMenuShowsFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := newRemoteMenuTui(t, srv)
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm, _ := m.Update(remoteModelsMsg{err: errors.New("ollama model list HTTP 401")})
	got := mm.(tuiModel)
	if got.modelBusy {
		t.Fatal("a failed fetch must stop the spinner")
	}
	if !strings.Contains(got.modelStatus, "401") {
		t.Fatalf("status = %q, want the error", got.modelStatus)
	}
	if len(got.modelItems) != 0 {
		t.Fatalf("a failed fetch must not invent items: %+v", got.modelItems)
	}
}

// Выбор модели пересобирает движок и печатает, что выбор живёт только в этой
// сессии: в файл он попадёт через setup → apply.
func TestRemoteModelMenuSwitchesModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:4b"}]}`))
	}))
	defer srv.Close()

	m := newRemoteMenuTui(t, srv)
	oldEngine := m.s.engine
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm, _ := m.Update(remoteModelsMsg{models: []llm.RemoteModel{{ID: "qwen3:4b"}}})
	m = mm.(tuiModel)

	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.s.cfg.RemoteModel != "qwen3:4b" {
		t.Fatalf("remote model = %q, want the selected one", got.s.cfg.RemoteModel)
	}
	if got.s.engine == oldEngine {
		t.Fatal("the engine must be rebuilt: the model name is baked into it")
	}
	if !strings.Contains(got.content, "qwen3:4b") {
		t.Fatalf("the switch must be reported:\n%s", got.content)
	}
	if !got.modelItems[0].isCurrent {
		t.Fatal("the new model must be marked as current")
	}
}

// Локальный GGUF не имеет каталога: меню остаётся прежним, файлы и
// рекомендации на месте.
func TestLocalProviderMenuHasNoCatalog(t *testing.T) {
	isolateModelDir(t)
	m := newTestTui()
	m.s.provider = config.ProviderLocal
	got, cmd := m.openModelMenu()
	if cmd != nil {
		t.Fatal("the local menu must not start a fetch")
	}
	if got.modelBusy {
		t.Fatal("the local menu has nothing to load")
	}
	if len(got.modelItems) == 0 {
		t.Skip("no local models and no recommendations in this environment")
	}
	for _, it := range got.modelItems {
		if it.remote {
			t.Fatalf("a local menu must not list provider models: %+v", it)
		}
	}
}

// Каталог длиннее окна: список прокручивается вместе с выделением.
func TestRemoteModelMenuScrolls(t *testing.T) {
	m := newTestTui()
	m.s.provider = config.ProviderOllama
	m.width, m.height = 100, 40
	m.state = tuiModelMenu
	models := make([]llm.RemoteModel, 0, 40)
	for i := 0; i < 40; i++ {
		models = append(models, llm.RemoteModel{ID: fmt.Sprintf("model-%02d", i)})
	}
	m.applyRemoteModelsMsg(remoteModelsMsg{models: models})
	m.modelIdx = len(models) - 1
	out := m.render()
	if !strings.Contains(out, models[len(models)-1].ID) {
		t.Fatalf("the selected model must be visible:\n%s", out)
	}
	if strings.Contains(out, "(+") {
		t.Fatalf("the truncated marker must be gone:\n%s", out)
	}
}

// Загрузка каталога должна доходить до Update возвращаемым сообщением:
// раньше команда клала результат в канал, которого никто не читал, и меню
// вечно висело на «loading the model list…».
func TestRemoteModelMenuFetchDeliversThroughUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:8b"},{"id":"qwen3:4b"}]}`))
	}))
	defer srv.Close()

	m := newRemoteMenuTui(t, srv)
	mm, cmd := m.openModelMenu()
	if cmd == nil {
		t.Fatal("a remote menu must start a catalog fetch")
	}
	m2, _ := mm.Update(cmd())
	got := m2.(tuiModel)
	if got.modelBusy {
		t.Fatal("the menu must stop showing progress once the catalog arrives")
	}
	if len(got.modelItems) != 2 {
		t.Fatalf("items = %+v, want the fetched catalog", got.modelItems)
	}
	if !strings.Contains(got.render(), "qwen3:4b") {
		t.Fatalf("the catalog should be on screen:\n%s", got.render())
	}
}

// /models открывает из палитры то же окно, что Ctrl+O, и запускает ту же
// загрузку каталога: палитра ходит через handleSlash и не может разойтись.
func TestModelsSlashCommandOpensCatalogMenu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:4b"}]}`))
	}))
	defer srv.Close()

	m := newRemoteMenuTui(t, srv)
	mm, cmd := m.handleSlash("/models")
	got := mm.(tuiModel)
	if got.state != tuiModelMenu {
		t.Fatalf("state = %d, want tuiModelMenu", got.state)
	}
	if !got.modelBusy || cmd == nil {
		t.Fatal("/models must open the menu and start the catalog fetch")
	}
}

// /models локального провайдера не ходит в сеть и печатает локальный список.
func TestModelsSlashCommandLocalListsFiles(t *testing.T) {
	isolateModelDir(t)
	m := newTestTui()
	m.s.provider = config.ProviderLocal
	mm, cmd := m.handleSlash("/models")
	got := mm.(tuiModel)
	if cmd != nil {
		t.Fatal("the local menu must not start a fetch")
	}
	if got.state != tuiModelMenu || got.modelBusy {
		t.Fatalf("state = %d busy = %v, want an idle local menu", got.state, got.modelBusy)
	}
	if len(got.modelItems) == 0 {
		t.Skip("no local models and no recommendations in this environment")
	}
	for _, it := range got.modelItems {
		if it.remote {
			t.Fatalf("a local menu must not list provider models: %+v", it)
		}
	}
}

// /models вне TUI печатает каталог провайдера тем же запросом, что и меню.
func TestShowModelsPrintsRemoteCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:4b","owned_by":"qwen"},{"id":"deepcoder:1.5b"}]}`))
	}))
	defer srv.Close()

	s := &session{
		cfg:      config.Config{Provider: config.ProviderOllama, RemoteBaseURL: srv.URL},
		provider: config.ProviderOllama,
	}
	var out strings.Builder
	showModels(&out, s)
	text := out.String()
	for _, want := range []string{"qwen3:4b", "deepcoder:1.5b", "qwen"} {
		if !strings.Contains(text, want) {
			t.Fatalf("showModels output should mention %q:\n%s", want, text)
		}
	}
}

// /models локального провайдера вне TUI печатает рекомендованные модели.
func TestShowModelsListsLocalFiles(t *testing.T) {
	isolateModelDir(t)
	s := &session{
		cfg:      config.Config{Provider: config.ProviderLocal},
		provider: config.ProviderLocal,
	}
	var out strings.Builder
	showModels(&out, s)
	if !strings.Contains(out.String(), "Recommended") {
		t.Fatalf("local /models should list the local models:\n%s", out.String())
	}
}
