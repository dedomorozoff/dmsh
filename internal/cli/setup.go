package cli

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// Экран setup отвечает на вопрос «как dmsh вообще говорит с моделью»:
// локальный GGUF, удалённый API или «что есть». Провайдер выбирается один
// раз за сессию, поэтому применяется он кнопкой apply, а не стрелками:
// движок пересобирается, и случайное переключение оставило бы пользователя
// без модели.

// Строки экрана setup. Значение константы — индекс строки, по нему же
// считается позиция курсора.
const (
	supRowProvider = iota
	supRowRemote
	supRowLocal
	supRowEndpoint
	supRowAPIKey
	supRowApply
	supRowReload
	supRowCount
)

const setupTitle = "setup"

const setupFooter = "↑/↓ select · ←/→ provider · Enter edit/apply · Esc close (unsaved changes are dropped)"

// setupLabels — подписи строк. Ширина колонки задана здесь, поэтому длинные
// значения просто обрезаются по ширине окна, а не ломают рамку.
var setupLabels = [supRowCount]string{
	supRowProvider: "provider",
	supRowRemote:   "remote model",
	supRowLocal:    "local gguf",
	supRowEndpoint: "endpoint",
	supRowAPIKey:   "api key",
	supRowApply:    "apply",
	supRowReload:   "reload",
}

// setupProviders — порядок перебора провайдеров стрелками: от общего к
// частному, auto → удалённый → Ollama → локальный GGUF.
var setupProviders = []config.Provider{
	config.ProviderAuto,
	config.ProviderPollinations,
	config.ProviderOllama,
	config.ProviderLocal,
}

// openSetup открывает экран со значениями текущей сессии.
func (m tuiModel) openSetup() tuiModel {
	m.state = tuiSetup
	m.supIdx = supRowProvider
	m.supEditing = false
	m.supStatus = ""
	m.supStatusErr = false
	m.supProvider = configuredProvider(m.rf.cfg.Provider, m.s)
	m.supRemote = m.s.cfg.RemoteModel
	m.supLocal = firstNonEmpty(m.s.cfg.ModelPath, m.rf.cfg.ModelPath)
	m.supEndpoint = firstNonEmpty(m.s.cfg.RemoteBaseURL, m.rf.cfg.RemoteBaseURL)
	// Ключ в конфиге хранится только если его туда положили: переменная
	// окружения важнее, и записывать её в файл нельзя без явного желания.
	m.supKey = strings.TrimSpace(m.s.cfg.RemoteAPIKey)
	return m
}

// configuredProvider — провайдер ДЛЯ РЕДАКТИРОВАНИЯ в setup: значение из
// конфига (флаг/файл), а не уже разрешённое в сессии — иначе «auto» на
// экране превратился бы в конкретику, и запись назад изменила бы конфиг.
// Живой провайдер сессии спрашивается через currentProvider, не здесь.
func configuredProvider(shell config.Provider, s *session) config.Provider {
	if shell.Valid() {
		return shell
	}
	if s != nil && s.provider.Valid() {
		return s.provider
	}
	return config.ProviderAuto
}

func nextProvider(p config.Provider, dir int) config.Provider {
	for i, v := range setupProviders {
		if v == p {
			return setupProviders[(i+dir+len(setupProviders))%len(setupProviders)]
		}
	}
	return setupProviders[0]
}

// setupValue возвращает значение строки для показа.
func (m tuiModel) setupValue(row int) string {
	switch row {
	case supRowProvider:
		return string(m.supProvider)
	case supRowRemote:
		return orPlaceholder(m.supRemote, config.DefaultModelFor(m.supProvider))
	case supRowLocal:
		return orPlaceholder(shortModelName(m.supLocal), "not set — the first GGUF found")
	case supRowEndpoint:
		return remoteEndpoint(m.supProvider, m.supEndpoint)
	case supRowAPIKey:
		switch {
		case m.supKey != "":
			return strings.Repeat("*", 8)
		case m.s != nil && m.s.cfg.APIKeySource() == "env":
			return "from " + config.APIKeyEnv
		default:
			return "not set — Enter to enter a key"
		}
	case supRowApply:
		return "switch the engine now and write " + configFileName
	case supRowReload:
		return "reload values from the running session"
	default:
		return ""
	}
}

// setupRouteLine объясняет, что произойдёт с ближайшим запросом: «auto»
// звучит как «что-то», а на деле это конкретный выбор с конкретным адресом.
func (m tuiModel) setupRouteLine() string {
	switch m.supProvider {
	case config.ProviderLocal:
		return "next request: local GGUF " + shortModelName(m.supLocal)
	case config.ProviderOllama:
		return "next request: " + m.remoteModel() + " via " + remoteEndpoint(m.supProvider, m.supEndpoint) +
			" (no api key)"
	default:
		key := "no api key"
		if m.supKey != "" {
			key = "api key ********"
		} else if m.s != nil && m.s.cfg.APIKeySource() == "env" {
			key = "api key from " + config.APIKeyEnv
		}
		if m.supProvider == config.ProviderPollinations {
			return "next request: " + m.remoteModel() + " via " + remoteEndpoint(m.supProvider, m.supEndpoint) + " (" + key + ")"
		}
		return "next request: local GGUF if found, otherwise " + m.remoteModel()
	}
}

// remoteModel — модель, которая уйдёт в запрос: пустое поле означает дефолт
// выбранного провайдера.
func (m tuiModel) remoteModel() string {
	return firstNonEmpty(m.supRemote, config.DefaultModelFor(m.supProvider))
}

func (m tuiModel) setupRow(row int) string {
	marker, style := "  ", colorGray
	valueStyle := colorGray
	if row == m.supIdx {
		marker, style = "> ", colorYellow+colorBold
		if row < supRowApply {
			valueStyle = colorReset
		}
	}
	return fitWidth(fmt.Sprintf("%s%s%-13s%s%s %s", style, marker, setupLabels[row], colorReset, valueStyle, m.setupValue(row)), m.width)
}

// setupRows собирает содержимое окна: строки, маршрут запроса и подвал.
// Последней строкой идёт либо подвал, либо поле ввода.
func (m tuiModel) setupRows() []string {
	rows := make([]string, 0, supRowCount+2)
	for i := 0; i < supRowCount; i++ {
		rows = append(rows, m.setupRow(i))
	}
	rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", colorGray, m.setupRouteLine(), colorReset), m.width))
	switch {
	case m.supStatus != "":
		style := colorGreen
		if m.supStatusErr {
			style = colorRed
		}
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", style, m.supStatus, colorReset), m.width))
	case m.supEditing:
		rows = append(rows, m.setupEditLine())
	default:
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s", colorGray, setupFooter, colorReset), m.width))
	}
	return rows
}

// setupModal описывает окно setup для рендера.
func (m tuiModel) setupModal() modalBox {
	rows := m.setupRows()
	cursorY := m.supIdx
	cursorX := 2
	if m.supEditing {
		cursorY = len(rows) - 1
		r := []rune(m.input)
		cursorX = displayWidth(m.setupEditName()+"> ") + displayWidth(string(r[:min(m.cursorPos, len(r))]))
	}
	return modalBox{title: setupTitle, rows: rows, cursorX: cursorX, cursorY: cursorY}
}

func (m tuiModel) setupEditLine() string {
	return fmt.Sprintf("%s%s>%s%s%s", colorGray, m.setupEditName(), colorReset, colorGreen, m.input)
}

// setupEditName — имя строки, значение которой сейчас правится.
func (m tuiModel) setupEditName() string {
	if m.supIdx >= 0 && m.supIdx < supRowCount {
		return setupLabels[m.supIdx]
	}
	return "value"
}

// handleSetupKey обрабатывает клавиши на экране setup.
func (m tuiModel) handleSetupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.supEditing {
		return m.handleSetupEdit(msg)
	}
	switch {
	case msg.Code == tea.KeyEscape, msg.Keystroke() == "ctrl+c":
		return m.closeSetup(), nil
	case msg.Code == tea.KeyEnter:
		return m.setupEnter()
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.supIdx = (m.supIdx - 1 + supRowCount) % supRowCount
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.supIdx = (m.supIdx + 1) % supRowCount
		return m, nil
	case msg.Code == tea.KeyLeft || msg.Keystroke() == "ctrl+b":
		m.supProvider = nextProvider(m.supProvider, -1)
		return m, nil
	case msg.Code == tea.KeyRight || msg.Keystroke() == "ctrl+f":
		m.supProvider = nextProvider(m.supProvider, 1)
		return m, nil
	}
	return m, nil
}

// handleSetupEdit правит значение выбранной строки. Esc отменяет правку, а
// не всё окно.
func (m tuiModel) handleSetupEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.supEditing = false
		m.input = ""
		m.cursorPos = 0
		return m, nil
	case msg.Code == tea.KeyEnter:
		m.commitSetupEdit()
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

// commitSetupEdit забирает отредактированное значение и проверяет его до
// пересборки движка: опечатка в адресе должна быть видна сразу.
func (m *tuiModel) commitSetupEdit() {
	value := strings.TrimSpace(m.input)
	switch m.supIdx {
	case supRowRemote:
		m.supRemote = value
	case supRowLocal:
		m.supLocal = value
	case supRowEndpoint:
		m.supEndpoint = value
	case supRowAPIKey:
		m.supKey = value
	}
	m.supEditing = false
	m.input = ""
	m.cursorPos = 0
	if m.supIdx == supRowEndpoint {
		if err := config.ValidateRemoteBaseURL(remoteEndpoint(m.supProvider, value)); err != nil {
			m.supStatus = err.Error()
			m.supStatusErr = true
			return
		}
	}
	m.supStatus = ""
	m.supStatusErr = false
}

// setupEditValue — значение строки для правки: то, что лежит в полях, без
// показательных подписей. Пустое здесь значит «не задано».
func (m tuiModel) setupEditValue(row int) string {
	switch row {
	case supRowRemote:
		return m.supRemote
	case supRowLocal:
		return m.supLocal
	case supRowEndpoint:
		return m.supEndpoint
	case supRowAPIKey:
		return m.supKey
	default:
		return ""
	}
}

// setupEnter выполняет действие выбранной строки.
func (m tuiModel) setupEnter() (tea.Model, tea.Cmd) {
	switch m.supIdx {
	case supRowProvider:
		m.supProvider = nextProvider(m.supProvider, 1)
		return m, nil
	case supRowRemote, supRowLocal, supRowEndpoint, supRowAPIKey:
		m.supEditing = true
		// В поле ввода идёт настоящее значение, а не подпись вида
		// «not set — Enter…»: редактировать подпись бессмысленно.
		m.input = m.setupEditValue(m.supIdx)
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	case supRowApply:
		return m.applySetup()
	case supRowReload:
		m = m.openSetup()
		m.supStatus = "reloaded from the running session"
		return m, nil
	default:
		return m, nil
	}
}

// setupConfig собирает конфиг сессии из отредактированных строк поверх того,
// что уже сохранено на диске. Брать базой флаги запуска опасно: они могут
// быть неполными, и в конфиг попало бы что-то вроде top_p = 0.
func setupConfig(base config.Config, m tuiModel) config.Config {
	base.Provider = m.supProvider
	base.RemoteModel = strings.TrimSpace(m.supRemote)
	base.ModelPath = strings.TrimSpace(m.supLocal)
	base.RemoteBaseURL = strings.TrimSpace(m.supEndpoint)
	base.RemoteAPIKey = strings.TrimSpace(m.supKey)
	return base
}

// setupBase отдаёт конфиг, поверх которого записывается выбор: сперва тот, с
// которым реально работает сессия (он включает и значения из флагов запуска),
// а если тот почему-то не проходит проверку — сохранённый на диске.
func (m tuiModel) setupBase() (config.Config, error) {
	if err := m.rf.cfg.Validate(); err == nil {
		return m.rf.cfg, nil
	}
	return config.Load()
}

// applySetup переключает способ подключения к LLM. Значения проверяются и
// движок пересобирается до записи на диск: оборванная настройка не должна
// оставить пользователя без модели ни сейчас, ни в следующей сессии.
func (m tuiModel) applySetup() (tea.Model, tea.Cmd) {
	if !m.supProvider.Valid() {
		m.supStatus = fmt.Sprintf("invalid provider %q", m.supProvider)
		m.supStatusErr = true
		return m, nil
	}
	base, err := m.setupBase()
	if err != nil {
		m.supStatus = err.Error()
		m.supStatusErr = true
		return m, nil
	}
	cfg := setupConfig(base, m)
	if err := cfg.Validate(); err != nil {
		m.supStatus = err.Error()
		m.supStatusErr = true
		return m, nil
	}
	provider, notice, err := m.s.switchProvider(cfg)
	if err != nil {
		m.supStatus = err.Error()
		m.supStatusErr = true
		return m, nil
	}
	if err := config.Save(cfg); err != nil {
		m.supStatus = err.Error()
		m.supStatusErr = true
		return m, nil
	}
	m.rf.cfg = cfg
	m.supStatus = "connection: " + string(provider)
	if notice != "" {
		m.supStatus += " (" + notice + ")"
	}
	m.supStatusErr = false
	return m, nil
}

// closeSetup возвращает ввод в строку. Несохранённые правки отбрасываются,
// но пользователь об этом предупреждён.
func (m tuiModel) closeSetup() tuiModel {
	m.state = tuiIdle
	m.supEditing = false
	m.supStatus = ""
	m.input = ""
	m.cursorPos = 0
	if m.setupDirty() {
		m.addLine(fmt.Sprintf("%ssetup closed without applying (Ctrl+P → setup → apply)%s", colorGray, colorReset))
	}
	return m
}

// setupDirty сообщает, что строки разошлись с работающей сессией.
func (m tuiModel) setupDirty() bool {
	return m.supProvider != configuredProvider(m.rf.cfg.Provider, m.s) ||
		m.supRemote != strings.TrimSpace(m.s.cfg.RemoteModel) ||
		m.supLocal != firstNonEmpty(m.s.cfg.ModelPath, m.rf.cfg.ModelPath) ||
		m.supEndpoint != firstNonEmpty(m.s.cfg.RemoteBaseURL, m.rf.cfg.RemoteBaseURL) ||
		m.supKey != strings.TrimSpace(m.s.cfg.RemoteAPIKey)
}

// showSetup печатает, как dmsh подключается к модели, без интерактивного
// окна: в TUI тот же список открывается через Ctrl+P.
func showSetup(out io.Writer, s *session) {
	provider := currentProvider(s, "")
	fmt.Fprintf(out, "\n%s%s=== setup: how dmsh talks to the LLM ===%s\n", bold, cyan, reset)
	fmt.Fprintf(out, "  %sProvider:%s  %s\n", bold, reset, provider)
	if provider == config.ProviderLocal {
		fmt.Fprintf(out, "  %sLocal:%s     %s\n", bold, reset, orPlaceholder(shortModelName(s.cfg.ModelPath), "not found"))
	} else {
		fmt.Fprintf(out, "  %sRemote:%s    %s\n", bold, reset,
			firstNonEmpty(s.cfg.RemoteModel, config.DefaultModelFor(provider)))
		fmt.Fprintf(out, "  %sEndpoint:%s  %s\n", bold, reset, remoteEndpoint(provider, s.cfg.RemoteBaseURL))
	}
	if provider == config.ProviderOllama {
		fmt.Fprintf(out, "  %sAuth:%s      not needed (local server)\n", gray, reset)
		fmt.Fprintf(out, "  %sStart it:%s  ollama serve\n", gray, reset)
	} else {
		fmt.Fprintf(out, "  %sAuth:%s      %s\n", gray, reset, apiKeyLabel(provider, s.cfg))
	}
	fmt.Fprintf(out, "\n  change: dmsh --provider local|pollinations|ollama|auto\n")
	fmt.Fprintf(out, "  save:   dmsh config set provider ollama\n")
	fmt.Fprintf(out, "  key:    export %s=sk_...\n", config.APIKeyEnv)
	fmt.Fprintf(out, "  in TUI: Ctrl+P → setup\n\n")
}
