package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

// Строки экрана настроек. Значение константы — это индекс строки в списке,
// поэтому порядок важен: по нему же считается позиция курсора.
const (
	setRowMode = iota
	setRowProto
	setRowHost
	setRowPort
	setRowLogin
	setRowPass
	setRowNoProxy
	setRowEndpoint
	setRowTest
	setRowSave
	setRowReset
	setRowCount
)

// settingsTitle печатается в рамке модального окна.
const settingsTitle = "settings"

// settingsFooter объясняет управление и меняется на строку ввода, когда
// правится значение.
const settingsFooter = "↑/↓ select · ←/→ change · Enter edit/run · Esc close (unsaved changes are dropped)"

// configFileName — файл, куда пишет save.
const configFileName = "config.json"

// probeTimeout ограничивает проверку соединения: результат ждёт пользователь
// прямо в открытом окне.
const probeTimeout = 10 * time.Second

// proxyTestMsg — результат проверки соединения через текущие настройки.
type proxyTestMsg struct {
	via      string
	code     int
	duration time.Duration
	err      error
}

// settingLabels — подписи строк. Ширина колонки подсказок задана здесь,
// поэтому длинные подписи вроде "send one request…" просто обрезаются по
// ширине окна, а не ломают рамку.
var settingLabels = [setRowCount]string{
	setRowMode:     "proxy mode",
	setRowProto:    "protocol",
	setRowHost:     "host",
	setRowPort:     "port",
	setRowLogin:    "login",
	setRowPass:     "password",
	setRowNoProxy:  "no proxy",
	setRowEndpoint: "endpoint",
	setRowTest:     "test connection",
	setRowSave:     "save",
	setRowReset:    "reload",
}

// openSettings открывает экран настроек со значениями текущей сессии.
// Отредактированные значения не применяются, пока пользователь не нажмёт
// Enter на строке save: молчаливая смена прокси посреди сессии оборвала бы
// следующий запрос.
func (m tuiModel) openSettings() tuiModel {
	m.state = tuiSettings
	m.setIdx = setRowMode
	m.setEditing = false
	m.setStatus = ""
	m.setStatusErr = false
	m.setBusy = false
	return m.loadFromSession()
}

// loadFromSession заполняет строки значениями, с которыми реально работает
// сессия. Это база и для открытия экрана, и для строки reload.
func (m tuiModel) loadFromSession() tuiModel {
	p := m.sessionProxy()
	m.setMode = p.Mode
	m.setProto = p.Proto
	m.setHost = p.Host
	m.setPort = ""
	if p.Port > 0 {
		m.setPort = strconv.Itoa(p.Port)
	}
	m.setLogin = p.User
	m.setPass = p.Password
	m.setNoProxy = p.NoProxy
	if m.s != nil {
		m.setEndpoint = firstNonEmpty(m.s.cfg.RemoteBaseURL, m.rf.cfg.RemoteBaseURL)
	} else {
		m.setEndpoint = m.rf.cfg.RemoteBaseURL
	}
	return m
}

// sessionProxy возвращает настройки прокси, с которыми реально работает
// сессия: после переключения модели или применения настроек они могут
// отличаться от флагов запуска.
func (m tuiModel) sessionProxy() netproxy.Settings {
	if m.s != nil {
		return m.s.cfg.Proxy()
	}
	return m.rf.cfg.Proxy()
}

// endpoint — адрес, в который уйдёт удалённый запрос. Пустое поле значит
// «дефолт провайдера», поэтому его подставляет провайдер, а не конфиг.
func (m tuiModel) endpoint() string {
	provider := m.rf.cfg.Provider
	if m.s != nil && m.s.provider != "" {
		provider = m.s.provider
	}
	return remoteEndpoint(provider, m.setEndpoint)
}

// editedPort читает порт из строки ввода. Пустая строка — «порт не задан».
func (m tuiModel) editedPort() (int, error) {
	raw := strings.TrimSpace(m.setPort)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", raw)
	}
	return n, nil
}

// editedProxy собирает настройки прокси из строк экрана.
func (m tuiModel) editedProxy() netproxy.Settings {
	port, _ := m.editedPort() // ошибку порта ловит validateEdited
	return netproxy.Settings{
		Mode:     m.setMode,
		Proto:    m.setProto,
		Host:     m.setHost,
		Port:     port,
		User:     m.setLogin,
		Password: m.setPass,
		NoProxy:  m.setNoProxy,
	}.Normalize()
}

// setValue возвращает значение строки для показа. Пароль не показывается:
// видно только «задан», иначе строка утекает на общий скриншот.
func (m tuiModel) setValue(row int) string {
	switch row {
	case setRowMode:
		return string(m.setMode)
	case setRowProto:
		return orPlaceholder(m.setProto, netproxy.ProtoHTTP)
	case setRowHost:
		return orPlaceholder(m.setHost, "not set — Enter to enter the proxy address")
	case setRowPort:
		return orPlaceholder(m.setPort, "default")
	case setRowLogin:
		return orPlaceholder(m.setLogin, "not set")
	case setRowPass:
		return orPlaceholder(netproxy.Settings{Password: m.setPass}.MaskedPassword(), "not set")
	case setRowNoProxy:
		return orPlaceholder(m.setNoProxy, "not set — Enter to enter hosts to bypass")
	case setRowEndpoint:
		return orPlaceholder(m.setEndpoint, "not set — Enter to enter an OpenAI-compatible endpoint")
	case setRowTest:
		if m.setBusy {
			return "checking…"
		}
		return "send one request and report the route"
	case setRowSave:
		return "apply to this session and write " + configFileName
	case setRowReset:
		return "reload values from " + configFileName
	default:
		return ""
	}
}

func orPlaceholder(v, placeholder string) string {
	if strings.TrimSpace(v) == "" {
		return placeholder
	}
	return v
}

// effectiveLine показывает, куда уйдёт следующий запрос. Это то, что чаще
// всего нужно понять при настройке: режим auto молча берёт адрес из
// переменных окружения, и забытый HTTPS_PROXY выглядит как «прокси нет».
func (m tuiModel) effectiveLine() string {
	via, err := m.editedProxy().Effective(m.endpoint())
	if err != nil {
		return "invalid settings: " + err.Error()
	}
	return "next request: " + proxyRouteLabel(via)
}

// settingRow рисует одну строку настроек. Каждая строка — ровно одна
// строка окна: позиция курсора считается по индексу, поэтому перенос здесь
// ломал бы и выделение, и рамку.
func (m tuiModel) settingRow(row int) string {
	marker, style := "  ", colorGray
	valueStyle := colorGray
	if row == m.setIdx {
		marker, style = "> ", colorYellow+colorBold
		if row < setRowTest {
			valueStyle = colorReset
		}
	}
	return fitWidth(fmt.Sprintf("%s%s%-13s%s%s %s", style, marker, settingLabels[row], colorReset, valueStyle, m.setValue(row)), m.width)
}

// settingsRows собирает содержимое окна настроек: строки, пояснение о
// маршруте и подвал. Последней строкой идёт либо подвал, либо поле ввода —
// на неё и ставится курсор при правке значения.
func (m tuiModel) settingsRows() []string {
	rows := make([]string, 0, setRowCount+2)
	for i := 0; i < setRowCount; i++ {
		rows = append(rows, m.settingRow(i))
	}
	rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", colorGray, m.effectiveLine(), colorReset), m.width))
	switch {
	case m.setBusy:
		rows = append(rows, fitWidth(fmt.Sprintf("%s… checking connection (Esc cancels)%s", colorGray, colorReset), m.width))
	case m.setStatus != "":
		style := colorGreen
		if m.setStatusErr {
			style = colorRed
		}
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", style, m.setStatus, colorReset), m.width))
	case m.setEditing:
		rows = append(rows, m.editLine())
	default:
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", colorGray, settingsFooter, colorReset), m.width))
	}
	return rows
}

// settingsModal описывает окно настроек для рендера.
func (m tuiModel) settingsModal() modalBox {
	rows := m.settingsRows()
	cursorY := m.setIdx
	cursorX := 2
	if m.setEditing {
		// Во время правки курсор стоит на поле ввода внизу окна.
		cursorY = len(rows) - 1
		r := []rune(m.input)
		cursorX = displayWidth(m.setEditingName()+"> ") + displayWidth(string(r[:min(m.cursorPos, len(r))]))
	}
	return modalBox{title: settingsTitle, rows: rows, cursorX: cursorX, cursorY: cursorY}
}

// editLine — строка ввода значения: тот же приём, что и в палитре, где
// запрос живёт в обычной строке ввода.
func (m tuiModel) editLine() string {
	return fmt.Sprintf("%s%s%s%s%s", colorGray, m.setEditingName()+"> ", colorReset, colorGreen, m.input)
}

// setEditingName — имя строки, значение которой сейчас правится.
func (m tuiModel) setEditingName() string {
	if m.setIdx >= 0 && m.setIdx < setRowCount {
		return settingLabels[m.setIdx]
	}
	return "value"
}

// handleSettingsKey обрабатывает клавиши на экране настроек.
func (m tuiModel) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.setEditing {
		return m.handleSettingsEdit(msg)
	}

	switch {
	case msg.Code == tea.KeyEscape, msg.Keystroke() == "ctrl+c":
		return m.closeSettings(), nil
	case msg.Code == tea.KeyEnter:
		return m.settingsEnter()
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.setIdx = (m.setIdx - 1 + setRowCount) % setRowCount
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.setIdx = (m.setIdx + 1) % setRowCount
		return m, nil
	case msg.Code == tea.KeyLeft || msg.Keystroke() == "ctrl+b":
		return m.cycleSetting(-1), nil
	case msg.Code == tea.KeyRight || msg.Keystroke() == "ctrl+f":
		return m.cycleSetting(1), nil
	}
	return m, nil
}

// cycleSetting меняет переключаемое значение строки: режим и протокол —
// циклом, всё остальное правится только вводом.
func (m tuiModel) cycleSetting(dir int) tuiModel {
	switch m.setIdx {
	case setRowMode:
		if dir > 0 {
			m.setMode = m.setMode.Next()
		} else {
			m.setMode = m.setMode.Prev()
		}
	case setRowProto:
		if dir > 0 {
			m.setProto = netproxy.NextProto(m.setProto)
		} else {
			m.setProto = netproxy.PrevProto(m.setProto)
		}
	}
	return m
}

// handleSettingsEdit правит значение выбранной строки. Esc отменяет правку,
// а не всё окно: потерять напечатанный адрес из-за одного Esc обидно.
func (m tuiModel) handleSettingsEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.setEditing = false
		m.input = ""
		m.cursorPos = 0
		return m, nil
	case msg.Code == tea.KeyEnter:
		m.commitEdit()
		return m, nil
	case msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h":
		m.deleteBackward()
		return m, nil
	case msg.Code == tea.KeyDelete:
		m.deleteForward()
		return m, nil
	case msg.Code == tea.KeyLeft:
		if m.cursorPos > 0 {
			m.cursorPos--
		}
		return m, nil
	case msg.Code == tea.KeyRight:
		if m.cursorPos < runeSliceLen(m.input) {
			m.cursorPos++
		}
		return m, nil
	case msg.Code == tea.KeyHome:
		m.cursorPos = 0
		return m, nil
	case msg.Code == tea.KeyEnd:
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	}
	if isInputKey(msg) {
		m.input = insertAt(m.input, m.cursorPos, msg.Text)
		m.cursorPos += runeSliceLen(msg.Text)
	}
	return m, nil
}

// commitEdit записывает отредактированное значение в строку и проверяет
// настройки целиком: опечатка в хосте или порте должна быть видна сразу,
// до сохранения и следующего запроса.
func (m *tuiModel) commitEdit() {
	value := strings.TrimSpace(m.input)
	switch m.setIdx {
	case setRowHost:
		m.setHost = value
	case setRowPort:
		m.setPort = value
	case setRowLogin:
		m.setLogin = value
	case setRowPass:
		m.setPass = value
	case setRowNoProxy:
		m.setNoProxy = value
	case setRowEndpoint:
		m.setEndpoint = value
	}
	m.setEditing = false
	m.input = ""
	m.cursorPos = 0
	if err := m.validateEdited(); err != nil {
		m.setStatus = err.Error()
		m.setStatusErr = true
		return
	}
	m.setStatus = ""
	m.setStatusErr = false
}

// settingsEnter выполняет действие выбранной строки.
func (m tuiModel) settingsEnter() (tea.Model, tea.Cmd) {
	switch m.setIdx {
	case setRowMode, setRowProto:
		return m.cycleSetting(1), nil
	case setRowHost, setRowPort, setRowLogin, setRowPass, setRowNoProxy, setRowEndpoint:
		m.setEditing = true
		switch m.setIdx {
		case setRowHost:
			m.input = m.setHost
		case setRowPort:
			m.input = m.setPort
		case setRowLogin:
			m.input = m.setLogin
		case setRowPass:
			m.input = m.setPass
		case setRowNoProxy:
			m.input = m.setNoProxy
		case setRowEndpoint:
			m.input = m.setEndpoint
		}
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	case setRowTest:
		return m.startProxyTest()
	case setRowSave:
		return m.saveSettings()
	case setRowReset:
		m = m.loadFromSession()
		m.setStatus = "reloaded from the current session"
		m.setStatusErr = false
		return m, nil
	default:
		return m, nil
	}
}

// validateEdited проверяет собранные настройки без применения. Проверяются
// только меняемые поля: полный конфиг здесь не подходит — на экране могут
// быть незаполненные значения, которых в обычной сессии не бывает.
func (m tuiModel) validateEdited() error {
	if _, err := m.editedPort(); err != nil {
		return err
	}
	if err := m.editedProxy().Validate(); err != nil {
		return err
	}
	return config.ValidateRemoteBaseURL(m.endpoint())
}

// applySettings применяет отредактированные строки к сессии и пишет их в
// конфиг. Порядок важен: сначала проверяем будущий конфиг целиком, и только
// потом трогаем движок — иначе пользователь остался бы без модели.
func (m tuiModel) applySettings() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.SetProxy(m.editedProxy())
	cfg.RemoteBaseURL = m.setEndpoint
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := m.s.applyProxy(cfg.Proxy()); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.RemoteBaseURL) != strings.TrimSpace(m.s.cfg.RemoteBaseURL) {
		if err := m.s.applyRemoteBaseURL(cfg.RemoteBaseURL); err != nil {
			return err
		}
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	m.rf.cfg = cfg
	p := cfg.Proxy()
	m.setMode, m.setProto, m.setHost = p.Mode, p.Proto, p.Host
	m.setPort = ""
	if p.Port > 0 {
		m.setPort = strconv.Itoa(p.Port)
	}
	m.setLogin, m.setPass, m.setNoProxy = p.User, p.Password, p.NoProxy
	m.setEndpoint = cfg.RemoteBaseURL
	return nil
}

func (m tuiModel) saveSettings() (tea.Model, tea.Cmd) {
	if err := m.applySettings(); err != nil {
		m.setStatus = err.Error()
		m.setStatusErr = true
		return m, nil
	}
	m.setStatus = "saved: " + m.effectiveLine()
	m.setStatusErr = false
	return m, nil
}

// startProxyTest проверяет связность с endpoint'ом через текущие строки
// настроек, ещё не применяя их: сначала убедиться, что прокси работает, и
// только потом на него переключаться.
func (m tuiModel) startProxyTest() (tea.Model, tea.Cmd) {
	if m.setBusy {
		return m, nil
	}
	if err := m.validateEdited(); err != nil {
		m.setStatus = err.Error()
		m.setStatusErr = true
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.setCancel = cancel
	m.setBusy = true
	m.setStatus = ""
	m.setStatusErr = false
	endpoint := m.endpoint()
	proxy := m.editedProxy()
	return m, func() tea.Msg {
		start := time.Now()
		via, code, err := probeProxy(ctx, proxy, endpoint)
		return proxyTestMsg{via: via, code: code, duration: time.Since(start), err: err}
	}
}

// finishProxyTest печатает результат проверки.
func (m *tuiModel) finishProxyTest(msg proxyTestMsg) {
	m.setBusy = false
	if m.setCancel != nil {
		m.setCancel()
		m.setCancel = nil
	}
	if msg.err != nil {
		m.setStatus = fmt.Sprintf("not reachable: %v", msg.err)
		m.setStatusErr = true
		return
	}
	m.setStatus = fmt.Sprintf("reachable: HTTP %d in %s, %s", msg.code, msg.duration.Round(100*time.Millisecond), proxyRouteLabel(msg.via))
	m.setStatusErr = false
}

// closeSettings возвращает экран ввода. Несохранённые правки отбрасываются,
// но пользователь об этом предупреждён — иначе молчаливая потеря адреса
// выглядела бы как баг.
func (m tuiModel) closeSettings() tuiModel {
	m.state = tuiIdle
	m.setEditing = false
	m.setBusy = false
	m.setStatus = ""
	if m.setCancel != nil {
		m.setCancel()
		m.setCancel = nil
	}
	m.input = ""
	m.cursorPos = 0
	if m.dirty() {
		m.addLine(fmt.Sprintf("%ssettings closed without saving (Ctrl+P → settings → save)%s", colorGray, colorReset))
	}
	return m
}

// dirty сообщает, что строки настроек разошлись с применёнными.
func (m tuiModel) dirty() bool {
	p := m.sessionProxy()
	cur := m.editedProxy()
	if cur.Mode != p.Mode || cur.Proto != p.Proto || cur.Host != p.Host || cur.Port != p.Port ||
		cur.User != p.User || cur.Password != p.Password ||
		strings.TrimSpace(cur.NoProxy) != strings.TrimSpace(p.NoProxy) {
		return true
	}
	return strings.TrimSpace(m.setEndpoint) != strings.TrimSpace(m.s.cfg.RemoteBaseURL)
}

// probeProxy делает один запрос к endpoint'у через указанные настройки и
// возвращает использованный прокси и код ответа. Ответ сервера не важен:
// важно, что соединение установлено — любой HTTP-код доказывает, что маршрут
// живой.
func probeProxy(ctx context.Context, p netproxy.Settings, endpoint string) (string, int, error) {
	via, err := p.Effective(endpoint)
	if err != nil {
		return "", 0, err
	}
	client, err := p.Client(probeTimeout)
	if err != nil {
		return via, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return via, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return via, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return via, resp.StatusCode, nil
}

// proxyRouteLabel — как показать маршрут запроса: пустой адрес означает
// прямое соединение.
func proxyRouteLabel(via string) string {
	if strings.TrimSpace(via) == "" {
		return "direct connection"
	}
	return "via " + via
}

// showSettings печатает настройки сети без интерактивного окна: в TUI тот
// же список открывается через Ctrl+P, а в пайпе остаётся только текст.
func showSettings(out io.Writer, s *session) {
	p := s.cfg.Proxy()
	endpoint := remoteEndpoint(s.provider, s.cfg.RemoteBaseURL)
	fmt.Fprintf(out, "\n%s%s=== settings ===%s\n", bold, cyan, reset)
	fmt.Fprintf(out, "  %sProxy mode:%s  %s — %s\n", bold, reset, p.Mode, p.Describe())
	fmt.Fprintf(out, "  %sProtocol:%s   %s\n", bold, reset, orPlaceholder(p.Proto, netproxy.ProtoHTTP))
	fmt.Fprintf(out, "  %sHost:%s       %s\n", bold, reset, orPlaceholder(p.Host, "not set"))
	fmt.Fprintf(out, "  %sPort:%s       %s\n", bold, reset, orPort(p.Port))
	fmt.Fprintf(out, "  %sLogin:%s      %s\n", bold, reset, orPlaceholder(p.User, "not set"))
	fmt.Fprintf(out, "  %sPassword:%s   %s\n", bold, reset, orPlaceholder(p.MaskedPassword(), "not set"))
	fmt.Fprintf(out, "  %sNo proxy:%s    %s\n", bold, reset, orPlaceholder(p.NoProxy, "not set"))
	fmt.Fprintf(out, "  %sEndpoint:%s    %s\n", bold, reset, endpoint)
	if via, err := p.Effective(endpoint); err != nil {
		fmt.Fprintf(out, "  %sRoute:%s       invalid settings: %v\n", red, reset, err)
	} else {
		fmt.Fprintf(out, "  %sRoute:%s       %s\n", bold, reset, proxyRouteLabel(via))
	}
	fmt.Fprintf(out, "\n  change: dmsh --proxy-mode custom --proxy-host 127.0.0.1 --proxy-port 8080\n")
	fmt.Fprintf(out, "  save:   dmsh config set proxy-host 127.0.0.1\n")
	fmt.Fprintf(out, "          dmsh config set proxy-port 8080\n")
	fmt.Fprintf(out, "          dmsh config set proxy-mode custom\n")
	fmt.Fprintf(out, "  in TUI: Ctrl+P → settings\n\n")
}

func orPort(p int) string {
	if p <= 0 {
		return "default"
	}
	return strconv.Itoa(p)
}