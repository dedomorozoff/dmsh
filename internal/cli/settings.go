package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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
