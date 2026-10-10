package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

// newSettingsTui — модель с удалённым провайдером: применение настроек
// пересобирает сетевой движок, и локальная модель в тесте была бы лишней.
func newSettingsTui() tuiModel {
	rf := &rootFlags{cfg: config.Config{Provider: config.ProviderPollinations, Mode: config.ModeAI}}
	s := &session{cfg: rf.cfg, provider: config.ProviderPollinations, engine: &captureEngine{}}
	m := NewTuiModel(rf, s).(tuiModel)
	return m.openSettings()
}

func openSettingsViaPalette(t *testing.T, m tuiModel) tuiModel {
	t.Helper()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = mm.(tuiModel)
	items := paletteMatches("settings")
	if len(items) != 1 || items[0].name != "/settings" {
		t.Fatalf("palette should match exactly /settings, got %+v", items)
	}
	mm, _ = m.handleKey(tea.KeyPressMsg{Text: "settings", Code: 's'})
	m = mm.(tuiModel)
	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	return mm.(tuiModel)
}

// editValue открывает строку на правку, печатает текст и подтверждает его.
func editValue(m tuiModel, row int, text string) tuiModel {
	m.setIdx = row
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(m, tea.KeyPressMsg{Text: text, Code: rune('a')})
	return press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestPaletteOpensSettings(t *testing.T) {
	m := newTestTui()
	m = openSettingsViaPalette(t, m)
	if m.state != tuiSettings {
		t.Fatalf("state = %v, want tuiSettings", m.state)
	}
	if m.setMode != netproxy.ModeAuto {
		t.Fatalf("initial mode = %q, want auto", m.setMode)
	}
	if m.setProto != netproxy.ProtoHTTP {
		t.Fatalf("initial protocol = %q, want http", m.setProto)
	}
}

func TestSettingsModeCyclesWithArrows(t *testing.T) {
	m := newSettingsTui()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.setMode != netproxy.ModeOff {
		t.Fatalf("after right: mode = %q, want off", m.setMode)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.setMode != netproxy.ModeCustom {
		t.Fatalf("after two lefts: mode = %q, want custom", m.setMode)
	}
}

// Протокол переключается отдельно от режима: socks5 — самый частый выбор
// после http, поэтому порядок http → https → socks5 → socks5h сохраняем.
func TestSettingsProtoCyclesWithArrows(t *testing.T) {
	m := newSettingsTui()
	m.setIdx = setRowProto
	for _, want := range []string{netproxy.ProtoHTTPS, netproxy.ProtoSOCKS5, netproxy.ProtoSOCKS5H, netproxy.ProtoHTTP} {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
		if m.setProto != want {
			t.Fatalf("protocol = %q, want %q", m.setProto, want)
		}
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.setProto != netproxy.ProtoSOCKS5H {
		t.Fatalf("protocol after left = %q, want socks5h", m.setProto)
	}
	// Стрелки не трогают обычные поля: там нужен ввод.
	m.setIdx = setRowHost
	m = press(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.setHost != "" {
		t.Fatalf("arrows must not edit the host field, got %q", m.setHost)
	}
}

func TestSettingsFieldsAreEditable(t *testing.T) {
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setProto = netproxy.ProtoSOCKS5
	m = editValue(m, setRowHost, "10.0.0.1")
	m = editValue(m, setRowPort, "1080")
	m = editValue(m, setRowLogin, "dmsh")
	m = editValue(m, setRowPass, "secret")
	if m.setHost != "10.0.0.1" || m.setPort != "1080" || m.setLogin != "dmsh" || m.setPass != "secret" {
		t.Fatalf("fields = %q/%q/%q/%q", m.setHost, m.setPort, m.setLogin, m.setPass)
	}
	p := m.editedProxy()
	if p.Proto != netproxy.ProtoSOCKS5 || p.Host != "10.0.0.1" || p.Port != 1080 || p.User != "dmsh" || p.Password != "secret" {
		t.Fatalf("editedProxy() = %+v", p)
	}
	if m.setStatusErr {
		t.Fatalf("valid fields reported an error: %s", m.setStatus)
	}
}

func TestSettingsPasswordIsMaskedInList(t *testing.T) {
	m := newSettingsTui()
	m = editValue(m, setRowPass, "secret")
	rows := strings.Join(m.settingsRows(), "\n")
	if strings.Contains(rows, "secret") {
		t.Fatalf("password must not be shown in the list:\n%s", rows)
	}
	if !strings.Contains(rows, "****") {
		t.Fatalf("password row should show a mask:\n%s", rows)
	}
	// Во время ввода пароль виден: иначе опечатку не заметить.
	m.setIdx = setRowPass
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	if !strings.Contains(m.editLine(), "x") {
		t.Fatalf("password should be visible while editing: %q", m.editLine())
	}
}

func TestSettingsBadPortIsRejected(t *testing.T) {
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setHost = "10.0.0.1"
	m = editValue(m, setRowPort, "eighty")
	if !m.setStatusErr {
		t.Fatal("a non-numeric port must be reported as an error")
	}
}

func TestSettingsCustomWithoutHostIsRejected(t *testing.T) {
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setHost = ""
	m.setIdx = setRowSave
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	if !m.setStatusErr {
		t.Fatal("proxy_mode=custom without a host must not be applied silently")
	}
	if m.s.cfg.Proxy().Mode == netproxy.ModeCustom {
		t.Fatal("session must not switch to custom without a host")
	}
}

func TestSettingsEditEscapeKeepsOldValue(t *testing.T) {
	m := newSettingsTui()
	m.setIdx = setRowHost
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.setEditing {
		t.Fatal("Escape should leave edit mode")
	}
	if m.state != tuiSettings {
		t.Fatal("Escape in edit mode must not close the whole window")
	}
	if m.setHost != "" {
		t.Fatalf("host = %q, want the value unchanged", m.setHost)
	}
}

func TestSettingsSaveAppliesAndPersists(t *testing.T) {
	isolateModelDir(t)
	m := newSettingsTui()
	// Движок до сохранения: после смены прокси у него другой HTTP-клиент,
	// поэтому он обязан быть пересобран, а не переиспользован.
	oldEngine := m.s.engine
	m.setMode = netproxy.ModeCustom
	m.setProto = netproxy.ProtoSOCKS5
	m.setHost = "10.0.0.1"
	m.setPort = "1080"
	m.setLogin = "dmsh"
	m.setPass = "secret"
	m.setNoProxy = "localhost"
	m.setIdx = setRowSave
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	if m.setStatusErr {
		t.Fatalf("save failed: %s", m.setStatus)
	}
	if m.s.engine == oldEngine {
		t.Fatal("engine must be rebuilt after applying proxy settings")
	}

	p := m.s.cfg.Proxy()
	if p.Mode != netproxy.ModeCustom || p.Proto != netproxy.ProtoSOCKS5 || p.Host != "10.0.0.1" ||
		p.Port != 1080 || p.User != "dmsh" || p.Password != "secret" || p.NoProxy != "localhost" {
		t.Fatalf("session proxy = %+v, want the saved values", p)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Proxy()
	if got.Mode != netproxy.ModeCustom || got.Proto != netproxy.ProtoSOCKS5 || got.Host != "10.0.0.1" || got.Port != 1080 {
		t.Fatalf("persisted config = %+v, want the saved values", got)
	}
	if got.User != "dmsh" || got.Password != "secret" {
		t.Fatalf("persisted credentials = %q/%q", got.User, got.Password)
	}
	if !strings.Contains(got.NoProxy, "localhost") {
		t.Fatalf("persisted no_proxy = %q", got.NoProxy)
	}
}

func TestSettingsCloseWithoutSaveKeepsSession(t *testing.T) {
	isolateModelDir(t)
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setHost = "10.0.0.1"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %v, want tuiIdle", m.state)
	}
	if m.s.cfg.Proxy().Host != "" {
		t.Fatal("unsaved edits must not reach the session")
	}
	if !strings.Contains(m.content, "without saving") {
		t.Fatalf("closing with unsaved edits should warn the user, got:\n%s", m.content)
	}
}

func TestSettingsCloseAfterSaveIsSilent(t *testing.T) {
	isolateModelDir(t)
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setHost = "10.0.0.1"
	m.setIdx = setRowSave
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(m.content, "without saving") {
		t.Fatalf("saved settings should close silently, got:\n%s", m.content)
	}
}

func TestSettingsReloadDropsEdits(t *testing.T) {
	m := newSettingsTui()
	m.setMode = netproxy.ModeCustom
	m.setHost = "10.0.0.1"
	m.setIdx = setRowReset
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	if m.setHost != "" || m.setMode != netproxy.ModeAuto {
		t.Fatalf("reload should restore session values, got %q/%q", m.setMode, m.setHost)
	}
	if m.setStatusErr {
		t.Fatalf("reload failed: %s", m.setStatus)
	}
}

func TestSettingsRowsAreSingleLines(t *testing.T) {
	m := newSettingsTui()
	m.width = 100
	for i, row := range m.settingsRows() {
		if strings.Contains(row, "\n") {
			t.Fatalf("row %d spans several lines: %q", i, row)
		}
	}
	if got, want := m.settingsModal().cursorY, setRowMode; got != want {
		t.Fatalf("modal cursor row = %d, want %d", got, want)
	}
	m.setEditing = true
	m.input = "http://x"
	m.cursorPos = runeSliceLen(m.input)
	box := m.settingsModal()
	if got, want := box.cursorY, len(box.rows)-1; got != want {
		t.Fatalf("modal cursor row while editing = %d, want %d (the input line)", got, want)
	}
	if box.cursorX < len("host> ") {
		t.Fatalf("modal cursor column = %d, want at least the prompt width", box.cursorX)
	}
}

func TestSettingsTestConnectionReportsRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	port, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatalf("parse proxy port: %v", err)
	}
	p := netproxy.Settings{Mode: netproxy.ModeCustom, Host: proxyURL.Hostname(), Port: port}
	gotVia, code, err := probeProxy(context.Background(), p, srv.URL)
	if err != nil {
		t.Fatalf("probeProxy: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if gotVia != proxy.URL {
		t.Fatalf("via = %q, want %q", gotVia, proxy.URL)
	}
}

func TestSettingsTestConnectionDirectWhenBypassed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p := netproxy.Settings{Mode: netproxy.ModeCustom, Host: "127.0.0.1", Port: 1, NoProxy: "127.0.0.1"}
	gotVia, code, err := probeProxy(context.Background(), p, srv.URL)
	if err != nil {
		t.Fatalf("probeProxy: %v", err)
	}
	if code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", code)
	}
	if gotVia != "" {
		t.Fatalf("via = %q, want empty (bypassed)", gotVia)
	}
}

func TestShowSettingsPrintsRoute(t *testing.T) {
	var out strings.Builder
	showSettings(&out, &session{cfg: config.Config{
		ProxyMode:     netproxy.ModeCustom,
		ProxyProto:    netproxy.ProtoSOCKS5,
		ProxyHost:     "proxy.local",
		ProxyPort:     1080,
		ProxyUser:     "user",
		ProxyPassword: "secret",
	}})
	text := out.String()
	if strings.Contains(text, "secret") {
		t.Fatalf("settings output leaked the proxy password:\n%s", text)
	}
	for _, want := range []string{"custom", "socks5", "proxy.local", "1080", "user", "Route"} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings output should mention %q:\n%s", want, text)
		}
	}
}