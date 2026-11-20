package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

type tuiState int

const (
	tuiIdle tuiState = iota
	tuiStreaming
	tuiConfirming
	tuiQuestion
	tuiSearch
	tuiModelMenu
	tuiPalette
	tuiSettings
	tuiSetup
	tuiModeMenu
	tuiDirMenu  // выбор папки кликом по текущей папке в статусной строке
	tuiTodoMenu // список задач модели с чекбоксами
	tuiTerminal // терминал в окне приложения: экран занимает весь кадр
	tuiRunning  // команда выполняется: ввод свободен, виден спиннер
	tuiFind     // поиск по выводу: запрос в строке ввода, совпадения подсвечены
)

type tuiModel struct {
	rf   *rootFlags
	s    *session
	errW io.Writer

	input     string
	cursorPos int
	history   []string
	histIdx   int

	content   string
	modeLabel string
	// scrollbackLines — сколько строк в content с последней обрезки. Сам
	// транскрипт не ограничен: его обрезает scrollback.go.
	scrollbackLines int
	// bodyCache — транскрипт, уже свёрнутый по ширине. Указатель, потому что
	// модель копируется по значению: кеш должен переживать копирование.
	bodyCache *bodyWrapCache

	width  int
	height int

	state            tuiState
	streaming        bool
	streamed         bool
	streamHasContent bool
	streamCtx        context.Context
	streamCancel     context.CancelFunc
	tokenCh          chan tokenMsg
	streamDone       chan streamResult

	confirmText string
	confirmOK   func(bool)
	// confirmReason — правило, которое потребовало подтверждения. Без него
	// окно спрашивает «точно ли?», не объясняя, кто и почему.
	confirmReason string
	pendingResp   prompt.Response
	// clarify ведёт диалог уточнений текущего запроса: модель не должна
	// спрашивать одно и то же и не должна спрашивать бесконечно.
	clarify clarification

	searchQuery string
	searchIdx   int
	// findQuery — поиск по выводу. Живёт и после закрытия окна поиска:
	// подсветка остаётся, пока её не снимут Esc, а F3 листает совпадения.
	findQuery   string
	findMatches []int
	findIdx     int

	tabMatches []string
	tabIdx     int

	// command palette (Ctrl+P): запрос живёт в обычной строке ввода,
	// paletteSaved — то, что там было до открытия.
	paletteSaved string
	paletteIdx   int

	// model menu (Ctrl+O)
	modelItems    []modelMenuItem
	modelIdx      int
	modelBusy     bool
	modelStatus   string
	modelProgress int
	modelCancel   context.CancelFunc
	modelCh       chan modelProgMsg
	modelDoneCh   chan modelDoneMsg
	// modelFilter — подстрока поиска по списку моделей.
	modelFilter string

	// settings screen (Ctrl+P → settings): правки прокси и endpoint'а.
	// Применяются только по Enter на строке save.
	setIdx       int
	setMode      netproxy.Mode
	setProto     string
	setHost      string
	setPort      string
	setLogin     string
	setPass      string
	setNoProxy   string
	setEndpoint  string
	setEditing   bool
	setBusy      bool
	setStatus    string
	setStatusErr bool
	setCancel    context.CancelFunc

	// setup screen (Ctrl+P → setup): выбор способа подключения к LLM.
	// Применяется по Enter на строке apply.
	supIdx      int
	supProvider config.Provider
	supRemote   string
	supLocal    string
	supEndpoint string
	// supKey — ключ, который будет записан в конфиг. Значение из переменной
	// окружения сюда не попадает: переносить секрет в файл без явного
	// желания нельзя.
	supKey       string
	supEditing   bool
	supStatus    string
	supStatusErr bool

	// dirMenu — выбор папки (клик по папке в статусной строке). Список
	// каталогов перечитывается при каждом переходе, поэтому держим его
	// рядом с состоянием, а не кешируем на сессию.
	dirItems  []dirItem
	dirIdx    int
	dirFilter string
	dirErr    string

	// todoIdx — выделенная задача в окне задач модели.
	todoIdx int

	// actionCmd — команда последнего выполнения: после неё dmsh предлагает,
	// что делать дальше. Пусто — панели под ответом нет.
	actionCmd string

	// term — встроенный терминал режима shell. Не nil, только пока
	// псевдотерминал жив: экран терминала рисуется вместо транскрипта.
	term *terminalPane

	scrollOffset int

	// fixTries считает автокоррекции подряд. Без счётчика модель, которая
	// возвращает ту же падающую команду, запускает бесконечный цикл.
	fixTries int

	// running — фоновая команда (composer, тесты, сборка): пока она идёт,
	// ввод свободен, а состояние держит спиннер. cmdSeq нумерует запуски,
	// чтобы устаревший результат не затёр новый.
	running      bool
	cmdSeq       int
	cmdCancel    context.CancelFunc
	cmdCommand   string
	cmdStartedAt time.Time
	// cmdCh — живые куски вывода фоновой команды и её итог. Один канал на
	// оба: итог обязан прийти после последнего куска, иначе «готово» легло бы
	// поверх недописанного вывода.
	cmdCh chan tea.Msg
	// cmdLive — сколько байт вывода уже показано вживую, cmdTrunc — живой
	// вывод обрезан по лимиту. Итог дублировать уже показанное не должен.
	cmdLive  int
	cmdTrunc bool
}

type streamResult struct {
	resp prompt.Response
	raw  string
	err  error
}

type tokenMsg struct {
	text string
}

type streamDoneMsg struct {
	resp prompt.Response
	err  error
}

// cmdDoneMsg — фоновая команда завершилась. seq отбрасывает устаревшие
// результаты: пользователь мог запустить новую команду раньше.
type cmdDoneMsg struct {
	seq  int
	resp prompt.Response
	res  executor.Result
	dec  policyDecisionLite
}

// cmdOutMsg — кусок вывода фоновой команды, пришедший, пока она ещё пишет.
// Границы кусков не совпадают со строками: текст дописывается как есть.
type cmdOutMsg struct {
	seq   int
	text  string
	isErr bool
}

// cmdTickMsg — секундный тик фоновой команды: обновляет время «идёт…».
type cmdTickMsg struct{}

// directDoneMsg — прямая команда через "!" завершилась в фоне.
type directDoneMsg struct {
	seq int
	cmd string
	res executor.Result
}

// policyDecisionLite — копия решения политики для записи в аудит после
// фонового выполнения: сам объект policy.Decision не храним, чтобы не
// тащить зависимость в сообщения.
type policyDecisionLite struct {
	allowed bool
	risk    prompt.Risk
	reason  string
}

type tuiWriter struct {
	buf     *strings.Builder
	tokenCh chan<- tokenMsg
	last    time.Time
}

func (w *tuiWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	now := time.Now()
	if now.Sub(w.last) > 40*time.Millisecond {
		select {
		case w.tokenCh <- tokenMsg{text: w.buf.String()}:
			w.buf.Reset()
			w.last = now
		default:
		}
	}
	return len(p), nil
}

func NewTuiModel(rf *rootFlags, s *session) tea.Model {
	m := tuiModel{rf: rf, s: s, errW: io.Discard, modeLabel: string(rf.cfg.Mode), content: "", tabIdx: -1, bodyCache: &bodyWrapCache{width: -1}}
	m.history = m.persistedHistory()
	m.histIdx = len(m.history)
	return m
}

// persistedHistory читает историю прошлых сессий из файла. Стрелки вверх/вниз
// листают по ней, поэтому без загрузки файла история была бы видна только
// внутри одной сессии. Дубликаты подряд идущих одинаковых записей убираются:
// один и тот же Enter, повторённый дважды, не должен занимать два шага.
func (m tuiModel) persistedHistory() []string {
	entries, err := loadHistoryEntries(m.s.cfg.HistoryFile)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		cmd := strings.TrimSpace(e.Command)
		if cmd == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == cmd {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

func (m tuiModel) Init() tea.Cmd {
	return tea.RequestWindowSize
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTerminal()
		return m, nil
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg), nil
	case tokenMsg:
		if len(msg.text) > 0 {
			m.streamed = true
			m.streamHasContent = true
			m.appendScrollback(msg.text)
			// Ответ модели прокручивается сам: пока он пишется, пользователь
			// смотрит на него, а не читает лог команды выше.
			m.scrollOffset = 0
		}
		if m.streaming && m.tokenCh != nil {
			return m, waitForToken(m.tokenCh, m.streamDone)
		}
		return m, nil
	case streamDoneMsg:
		m.streaming = false
		m.streamed = false
		m.streamHasContent = false
		m.state = tuiIdle
		m.tokenCh = nil
		m.streamDone = nil
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
		}
		if msg.err != nil {
			m.content = appendLine(m.content, fmt.Sprintf("%sError: %v%s", colorRed, msg.err, colorReset))
			return m, nil
		}
		if msg.resp.Intent == prompt.IntentAskClarification {
			if msg.resp.Command != "" {
				return m.executeResponse(msg.resp)
			}
			question := strings.TrimSpace(msg.resp.Question)
			if question != "" {
				// Модель повторило вопрос или исчерпало круги: останавливаемся,
				// чтобы пользователь не отвечал на одно и то же по кругу.
				if m.clarify.exhausted() || m.clarify.repeatsLast(question) {
					m.addLine(fmt.Sprintf("%s(stopped: the model kept asking instead of answering — rephrase the request)%s", colorGray, colorReset))
					return m, nil
				}
				m.addLine(fmt.Sprintf("%s[dmsh] %s%s", colorCyan, msg.resp.Question, colorReset))
				m.state = tuiQuestion
				m.pendingResp = msg.resp
				m.input = ""
				m.cursorPos = 0
				return m, nil
			}
		}
		if msg.resp.Intent == prompt.IntentRunCommand && msg.resp.Command != "" {
			return m.executeResponse(msg.resp)
		}
		if msg.resp.Intent == prompt.IntentExplain && msg.resp.Command != "" {
			return m.executeResponse(msg.resp)
		}
		return m, nil
	case modelProgMsg:
		m.modelProgress = msg.pct
		if m.modelBusy && m.modelDoneCh != nil {
			return m, waitForModel(m.modelCh, m.modelDoneCh)
		}
		return m, nil
	case proxyTestMsg:
		// Результат проверки приходит в любой момент: если пользователь
		// уже закрыл экран, сообщение просто игнорируем.
		if m.state == tuiSettings {
			m.finishProxyTest(msg)
		} else if m.setCancel != nil {
			m.setCancel()
			m.setCancel = nil
		}
		return m, nil
	case terminalOutputMsg:
		// Вывод приходит кусками и рисуется только пока терминал открыт:
		// закрытую панель незачем кормить.
		if m.term == nil {
			return m, nil
		}
		_, _ = m.term.screen.Write(msg.data)
		return m, waitTerminal(m.term)
	case terminalExitMsg:
		return m.finishTerminal(msg), nil
	case terminalClosedMsg:
		return m, nil
	case cmdDoneMsg:
		return m.finishBackgroundCommand(msg)
	case cmdOutMsg:
		return m.appendCmdOutput(msg)
	case cmdTickMsg:
		// Тик обновляет время в строке ввода. Очередь при этом нужно
		// продолжать вычитывать: тик приходит в тот же канал, что и вывод,
		// и без нового pump он умирает — вместе с ним замирает и команда,
		// и её результат.
		return m, m.cmdPump()
	case directDoneMsg:
		return m.finishDirectCommand(msg)
	case modelDoneMsg:
		m.modelBusy = false
		m.modelProgress = -1
		m.modelStatus = ""
		if m.modelCancel != nil {
			m.modelCancel()
			m.modelCancel = nil
		}
		// Загрузка закончена: в каналы больше никто не пишет, и следующее
		// открытие меню не должно ждать их сообщений впустую.
		m.modelCh = nil
		m.modelDoneCh = nil
		if msg.err != nil {
			if msg.err == context.Canceled {
				m.addLine("(cancelled)")
			} else {
				m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.err, colorReset))
			}
			return m, nil
		}
		switch {
		case msg.pruned:
			m.modelItems = m.buildModelMenu()
			if m.modelIdx >= len(m.visibleModelItems()) {
				m.modelIdx = len(m.visibleModelItems()) - 1
			}
			if m.modelIdx < 0 {
				m.modelIdx = 0
			}
			if len(msg.names) == 0 {
				m.addLine("(nothing to prune)")
			} else {
				for _, n := range msg.names {
					m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, n))
				}
			}
			return m, nil
		case msg.removed:
			name := filepath.Base(msg.path)
			m.modelItems = m.buildModelMenu()
			vis := m.visibleModelItems()
			if m.modelIdx >= len(vis) {
				m.modelIdx = len(vis) - 1
			}
			if m.modelIdx < 0 {
				m.modelIdx = 0
			}
			m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, name))
			return m, nil
		case msg.verified:
			name := filepath.Base(msg.path)
			status := msg.status
			if status == "" {
				status = "done"
			}
			if strings.HasPrefix(status, "OK") {
				m.addLine(fmt.Sprintf("%s%s: %s%s", colorGreen, name, status, colorReset))
			} else {
				m.addLine(fmt.Sprintf("%s%s: %s%s", colorYellow, name, status, colorReset))
			}
			return m, nil
		case msg.downloaded:
			name := filepath.Base(msg.path)
			m.addLine(fmt.Sprintf("(downloaded %s)", name))
			return m.startModelLoad(name)
		default:
			name := filepath.Base(msg.path)
			m.modelItems = m.buildModelMenu()
			for i, it := range m.modelItems {
				if it.name == name {
					m.modelIdx = i
				}
			}
			m.addLine(fmt.Sprintf("(%smodel%s: %s)", colorCyan, colorReset, name))
			return m, nil
		}
	case remoteModelsMsg:
		// Каталог приходит после закрытия окна: сообщение просто игнорируем.
		if m.state != tuiModelMenu {
			return m, nil
		}
		m.applyRemoteModelsMsg(msg)
		return m, nil
	}
	return m, nil
}

// bodyWrapCache хранит транскрипт, свёрнутый по текущей ширине. Кадр
// перерисовывается на каждый токен модели, а пересборка четырёх тысяч строк
// дороже самого кадра. Транскрипт дописывается только в конец, поэтому
// кешируются все полные строки, а незаконченная последняя строка
// сворачивается заново.
type bodyWrapCache struct {
	width int
	// base — полные строки content, для которых посчитаны rows.
	base string
	rows []string
	// completeRows — сколько первых rows относится к base. Хвост после них
	// принадлежит незаконченной строке и при следующем кадре заменяется.
	completeRows int
	// highlight — запрос, которым подсвечены rows. Другой запрос требует
	// пересборки: старые строки содержат старые маркеры.
	highlight string
}

// handlePaste вставляет буфер обмена в то поле, которое сейчас активно.
// Раньше вставка работала только в строке ввода и в вопросе, поэтому ключ
// провайдера и пароль прокси приходилось набирать по букве.
func (m tuiModel) handlePaste(msg tea.PasteMsg) tuiModel {
	switch m.state {
	case tuiIdle, tuiQuestion:
		m.insertInput(msg.Content)
	case tuiSetup:
		if m.supEditing {
			m.insertInput(msg.Content)
		}
	case tuiSettings:
		if m.setEditing {
			m.insertInput(msg.Content)
		}
	case tuiPalette:
		m.input = insertAt(m.input, m.cursorPos, msg.Content)
		m.cursorPos = runeSliceLen(m.input)
		m.paletteIdx = 0
	case tuiModelMenu:
		// Пока идёт загрузка каталога фильтр не меняем: список всё равно
		// перерисовывается целиком по окончании загрузки.
		if !m.modelBusy {
			m.modelFilter += msg.Content
			m.modelIdx = 0
		}
	case tuiSearch:
		m.searchQuery += msg.Content
		m.applySearch()
	case tuiTerminal:
		if m.term != nil {
			m.term.writeText(msg.Content)
		}
	}
	return m
}

// insertInput вставляет текст в общую строку ввода. Промпт, ключ
// провайдера и пароль прокси правятся одним и тем же буфером, поэтому и
// вставка у них общая.
func (m *tuiModel) insertInput(text string) {
	m.input = insertAt(m.input, m.cursorPos, text)
	m.cursorPos += runeSliceLen(text)
	m.tabMatches = nil
}

func (m tuiModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Глобальные биндинги: работают в любом состоянии, включая меню,
	// поиск, подтверждение и стриминг. Иначе подсказки в help и статусной
	// строке врали бы в половине экранов.
	// F1 матчится и по коду, и по строке: часть терминалов присылает
	// shift+f1. Палитра не исключается — help нужен из любого экрана.
	if msg.Code == tea.KeyF1 || msg.Keystroke() == "f1" {
		// В режиме терминала транскрипта не видно: справка открывается ценой
		// выхода из терминала, и об этом сказано прямо.
		m = m.stopTerminal("(terminal session closed to show help)")
		m.showHelp()
		return m, nil
	}
	if msg.Keystroke() == "ctrl+q" {
		return m.quitTui()
	}
	// Палитра открывается отовсюду, где бы ни был курсор: статусная строка
	// обещает её на каждом экране, а в терминале Ctrl+P ушёл бы в оболочку.
	if msg.Keystroke() == "ctrl+p" && m.state != tuiPalette {
		return m.openPalette(), nil
	}
	if m.state == tuiPalette {
		return m.handlePaletteKey(msg)
	}
	if m.state == tuiSettings {
		return m.handleSettingsKey(msg)
	}
	if m.state == tuiSetup {
		return m.handleSetupKey(msg)
	}
	if m.state == tuiModeMenu {
		return m.handleModeMenuKey(msg)
	}
	if m.state == tuiDirMenu {
		return m.handleDirMenuKey(msg)
	}
	if m.state == tuiTodoMenu {
		return m.handleTodoMenuKey(msg)
	}
	if m.state == tuiTerminal {
		return m.handleTerminalKey(msg)
	}
	if m.state == tuiSearch {
		return m.handleSearchKey(msg)
	}
	if m.state == tuiFind {
		return m.handleFindKey(msg)
	}
	if msg.Code == tea.KeyF3 {
		// F3 листает найденное и после закрытия окна поиска: подсветка
		// остаётся на экране, пока её не снимут.
		dir := 1
		if msg.Mod&tea.ModShift != 0 {
			dir = -1
		}
		m.findStep(dir)
		return m, nil
	}
	if m.state == tuiConfirming {
		return m.handleConfirmKey(msg)
	}
	if m.state == tuiQuestion {
		return m.handleQuestionKey(msg)
	}
	if m.state == tuiModelMenu {
		return m.handleModelMenuKey(msg)
	}
	if m.streaming {
		if msg.Code == tea.KeyEsc || msg.String() == "ctrl+c" {
			if m.streamCancel != nil {
				m.streamCancel()
				m.streamCancel = nil
			}
			m.streaming = false
			m.state = tuiIdle
			m.tokenCh = nil
			m.streamDone = nil
			m.addLine("^C")
			return m, nil
		}
		return m, waitForToken(m.tokenCh, m.streamDone)
	}
	// Фоновая команда идёт: Esc/Ctrl+C отменяет её, остальной ввод свободен.
	if m.running {
		if msg.Code == tea.KeyEsc || msg.String() == "ctrl+c" {
			m.cancelCommand()
			m.state = tuiIdle
			m.addLine(fmt.Sprintf("%s^C (command cancelled)%s", colorYellow, colorReset))
			return m, nil
		}
	}
	// Printable text: no ctrl/alt modifiers (shift/caps are fine). Text may be
	// multi-byte (Cyrillic etc.), so gate on content, not byte length.
	if isInputKey(msg) {
		m.input = insertAt(m.input, m.cursorPos, msg.Text)
		m.cursorPos += runeSliceLen(msg.Text)
		m.tabMatches = nil
		return m, nil
	}
	switch msg.Code {
	case tea.KeyEnter:
		return m.handleEnter()
	case tea.KeyEsc:
		// Esc снимает подсветку поиска; без неё, как и раньше, ничего.
		m.clearFind()
		return m, nil
	case tea.KeyTab:
		// Shift+Tab приходит как KeyTab с модификатором Shift: он занят
		// под переключение режима, обычный Tab остаётся комплитом.
		if msg.Mod&tea.ModShift != 0 {
			return m.cycleMode()
		}
		return m.handleTab()
	case tea.KeyBackspace:
		m.deleteBackward()
	case tea.KeyDelete:
		m.deleteForward()
	case tea.KeyLeft:
		if m.cursorPos > 0 {
			m.cursorPos--
		}
	case tea.KeyRight:
		if m.cursorPos < runeSliceLen(m.input) {
			m.cursorPos++
		}
	case tea.KeyHome:
		m.cursorPos = 0
	case tea.KeyEnd:
		m.cursorPos = runeSliceLen(m.input)
	case tea.KeyUp:
		if m.menuSelect(-1) {
			return m, nil
		}
		return m.handleHistoryPrev()
	case tea.KeyDown:
		if m.menuSelect(1) {
			return m, nil
		}
		return m.handleHistoryNext()
	// Скролл вывода больше не висит на стрелках: они целиком отданы истории.
	// PgUp/PgDn работают всегда, независимо от содержимого строки ввода.
	case tea.KeyPgUp:
		m.scrollBy(-(m.height - 3))
		return m, nil
	case tea.KeyPgDown:
		m.scrollBy(m.height - 3)
		return m, nil
	}
	switch msg.Keystroke() {
	case "ctrl+a":
		m.cursorPos = 0
	case "ctrl+e":
		m.cursorPos = runeSliceLen(m.input)
	case "ctrl+b":
		if m.cursorPos > 0 {
			m.cursorPos--
		}
	case "ctrl+f":
		// Ctrl+F — поиск по выводу, как в браузере и редакторах. Перемещение
		// курсора вправо остаётся на стрелке (и на Ctrl+B влево).
		return m.startFind(m.findQuery)
	case "ctrl+n":
		if m.menuSelect(1) {
			return m, nil
		}
		return m.handleHistoryNext()
	case "ctrl+o":
		// Model selection / download menu, or the provider catalog.
		return m.openModelMenu()
	case "ctrl+i":
		// Windows conhost sends Tab as HT (0x09), which decodes as ctrl+i.
		return m.handleTab()
	case "ctrl+d":
		// Documented: Ctrl+D exits. With text on the line it deletes forward
		// (readline semantics); on an empty line it quits.
		if m.input == "" {
			return m, tea.Quit
		}
		m.deleteForward()
	case "ctrl+r":
		return m.startSearch()
	case "ctrl+s":
		return m.startSearch()
	case "ctrl+h":
		// Windows conhost reports the Backspace key as BS (ctrl+h).
		m.deleteBackward()
	case "ctrl+u":
		m.input = ""
		m.cursorPos = 0
	case "ctrl+k":
		if m.cursorPos < runeSliceLen(m.input) {
			r := []rune(m.input)
			m.input = string(r[:m.cursorPos])
		}
	case "ctrl+w":
		m.input = deleteWordBack(m.input, &m.cursorPos)
	case "ctrl+l":
		m.clearContent()
	case "ctrl+c":
		m.addLine("^C")
		m.input = ""
		m.cursorPos = 0
	case "alt+b":
		m.cursorPos = deleteWordBackPos(m.cursorPos, m.input)
	case "alt+f":
		m.cursorPos = nextWordPos(m.cursorPos, m.input)
	case "alt+d":
		m.input = deleteWordForward(m.input, &m.cursorPos)
	default:
		if m.showActionBar() {
			ks := strings.ToLower(msg.Keystroke())
			for i, key := range actionBarKeys {
				if key == ks {
					return m.runActionBar(i)
				}
			}
		}
	}
	return m, nil
}

// switchMode переводит сессию в режим mode. Конфиг оболочки, конфиг сессии
// и подпись в статусной строке меняются одним путём, а вместе с режимом
// terminal открывается встроенный терминал.
func (m tuiModel) switchMode(mode config.Mode) (tuiModel, tea.Cmd) {
	m.rf.cfg.Mode = mode
	m.s.cfg.Mode = mode
	m.modeLabel = modeLabel(mode)
	if mode == config.ModeShell {
		return m.startTerminal()
	}
	return m.stopTerminal(""), nil
}

// cycleMode переключает режим по кругу и печатает новый. Тот же путь, что
// и у /mode: конфиг оболочки, конфиг сессии и подпись в статусной строке
// должны разойтись не могут.
func (m tuiModel) cycleMode() (tea.Model, tea.Cmd) {
	next := nextMode(m.rf.cfg.Mode)
	mm, cmd := m.switchMode(next)
	m = mm
	m.addLine(fmt.Sprintf("%s[dmsh] mode: %s%s", colorCyan, modeLabel(next), colorReset))
	if next == config.ModeShell && m.term == nil {
		m.addLine(fmt.Sprintf("%sterminal unavailable — Shift+Tab to return%s", colorGray, colorReset))
	}
	if next == config.ModeHelp {
		m.addLine(fmt.Sprintf("%srun_command is disabled in help mode%s", colorGray, colorReset))
	}
	return m, cmd
}

// quitTui останавливает стриминг и завершает программу. Отмена контекста
// нужна, чтобы не оставить инференс работать после выхода.
// нужна, чтобы не оставить инференс работать после выхода.
func (m tuiModel) quitTui() (tea.Model, tea.Cmd) {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	if m.setCancel != nil {
		m.setCancel()
		m.setCancel = nil
	}
	m.streaming = false
	m.state = tuiIdle
	m = m.stopTerminal("")
	m.addLine("bye!")
	return m, tea.Quit
}

// menuList возвращает текущий список меню: активную последовательность
// выбора, либо свежий фильтр по введённому префиксу.
func (m tuiModel) menuList() []string {
	if len(m.tabMatches) > 0 {
		return m.tabMatches
	}
	return slashMatches(m.input)
}

// menuSelect перемещает выделение в меню на dir шагов (+1 вниз, -1 вверх) и
// подставляет выбранную команду в строку ввода. Возвращает false, если меню
// сейчас неактивно.
func (m *tuiModel) menuSelect(dir int) bool {
	matches := m.menuList()
	n := len(matches)
	if n == 0 {
		return false
	}
	m.tabMatches = matches
	if m.tabIdx < 0 {
		if dir > 0 {
			m.tabIdx = 0
		} else {
			m.tabIdx = n - 1
		}
	} else {
		m.tabIdx = (m.tabIdx + n + dir) % n
	}
	m.input = m.tabMatches[m.tabIdx]
	m.cursorPos = runeSliceLen(m.input)
	return true
}

func (m tuiModel) handleTab() (tea.Model, tea.Cmd) {
	if m.tabMatches == nil {
		matches := slashMatches(m.input)
		if len(matches) == 0 {
			return m, nil
		}
		m.tabMatches = matches
		m.tabIdx = -1
	}
	m.tabIdx = (m.tabIdx + 1) % len(m.tabMatches)
	m.input = m.tabMatches[m.tabIdx]
	m.cursorPos = runeSliceLen(m.input)
	return m, nil
}

func (m tuiModel) startSearch() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		m.addLine("(no history)")
		return m, nil
	}
	m.state = tuiSearch
	m.searchQuery = ""
	m.applySearch()
	return m, nil
}

func (m tuiModel) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "ctrl+y":
		m.state = tuiIdle
		m.searchQuery = ""
		if m.searchIdx >= 0 && m.searchIdx < len(m.history) {
			m.input = m.history[m.searchIdx]
		}
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	case "esc", "ctrl+c", "ctrl+g":
		m.state = tuiIdle
		m.searchQuery = ""
		m.searchIdx = -1
		m.input = ""
		m.cursorPos = 0
		return m, nil
	case "ctrl+r":
		if idx, ok := m.searchStep(m.searchIdx-1, -1); ok {
			m.searchIdx = idx
			m.input = m.history[idx]
			m.cursorPos = runeSliceLen(m.input)
		}
		return m, nil
	case "ctrl+s":
		if idx, ok := m.searchStep(m.searchIdx+1, 1); ok {
			m.searchIdx = idx
			m.input = m.history[idx]
			m.cursorPos = runeSliceLen(m.input)
		}
		return m, nil
	}
	if msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h" {
		if r := []rune(m.searchQuery); len(r) > 0 {
			m.searchQuery = string(r[:len(r)-1])
			m.applySearch()
		}
		return m, nil
	}
	if isInputKey(msg) {
		m.searchQuery += msg.Text
		m.applySearch()
	}
	return m, nil
}

// searchStep finds the first history entry containing searchQuery, scanning
// from `from` toward dir (-1 = older, +1 = newer).
func (m tuiModel) searchStep(from, dir int) (int, bool) {
	for i := from; i >= 0 && i < len(m.history); i += dir {
		if strings.Contains(m.history[i], m.searchQuery) {
			return i, true
		}
	}
	return -1, false
}

// applySearch re-runs the current query and selects the newest match.
func (m *tuiModel) applySearch() {
	idx, ok := m.searchStep(len(m.history)-1, -1)
	m.searchIdx = idx
	if ok {
		m.input = m.history[idx]
		m.cursorPos = runeSliceLen(m.input)
		return
	}
	m.input = ""
	m.cursorPos = 0
}

func (m tuiModel) searchPrompt() string {
	return fmt.Sprintf("%s(reverse-i-search)%s`%s': ", colorYellow, reset, m.searchQuery)
}

// startFind открывает поиск по выводу. Ctrl+F передаёт прошлый запрос:
// повторное открытие продолжает с того же места, а не начинает с нуля.
func (m tuiModel) startFind(query string) (tea.Model, tea.Cmd) {
	m.state = tuiFind
	m.input = query
	m.cursorPos = runeSliceLen(query)
	m.refreshFind()
	return m, nil
}

// clearFind убирает подсветку и список совпадений. Запрос живёт отдельно от
// строки ввода: после Enter подсветка должна остаться.
func (m *tuiModel) clearFind() {
	m.findQuery = ""
	m.findMatches = nil
	m.findIdx = 0
}

// refreshFind пересобирает совпадения по текущей строке ввода. Вызывается на
// каждом символе: подсветка и переход к свежему совпадению идут следом.
func (m *tuiModel) refreshFind() {
	m.findQuery = strings.TrimSpace(m.input)
	m.collectFind()
	if len(m.findMatches) > 0 {
		m.jumpToFindMatch()
	}
}

// collectFind ищет запрос по строкам транскрипта, не считая ANSI-коды.
// Индекс ставится на последнее совпадение: свежий вывод полезнее старого.
func (m *tuiModel) collectFind() {
	m.findMatches = m.findMatches[:0]
	if q := m.findQuery; q != "" {
		for i, line := range strings.Split(m.content, "\n") {
			if containsFold(ansi.Strip(line), q) {
				m.findMatches = append(m.findMatches, i)
			}
		}
	}
	m.findIdx = len(m.findMatches) - 1
	if m.findIdx < 0 {
		m.findIdx = 0
	}
}

// findStep переходит к следующему (+1) или предыдущему (-1) совпадению и
// прокручивает кадр так, чтобы оно было видно.
func (m *tuiModel) findStep(dir int) {
	if len(m.findMatches) == 0 {
		m.collectFind()
		if len(m.findMatches) == 0 {
			return
		}
		m.jumpToFindMatch()
		return
	}
	n := len(m.findMatches)
	m.findIdx = ((m.findIdx+dir)%n + n) % n
	m.jumpToFindMatch()
}

// jumpToFindMatch ставит текущее совпадение в середину окна. Строка ищется в
// координатах транскрипта, а scrollOffset считается в строках экрана: одна
// логическая строка может занять несколько.
func (m *tuiModel) jumpToFindMatch() {
	if m.height <= 0 || len(m.findMatches) == 0 {
		return
	}
	row := m.rowOfLine(m.findMatches[m.findIdx])
	rows, inRows := m.layoutRows()
	avail := m.height - 1 - len(inRows)
	if avail < 1 {
		avail = 1
	}
	maxOffset := len(rows) - avail
	if maxOffset < 0 {
		maxOffset = 0
	}
	off := len(rows) - avail - row + avail/2
	if off < 0 {
		off = 0
	}
	if off > maxOffset {
		off = maxOffset
	}
	m.scrollOffset = off
}

// findPrompt показывает счётчик совпадений. Без него окно поиска молчит:
// непонятно, нашлось ли что-нибудь и где ты в списке.
func (m tuiModel) findPrompt() string {
	if len(m.findMatches) == 0 {
		if m.findQuery != "" {
			return fmt.Sprintf("%s(find: no matches)%s > ", colorYellow, colorReset)
		}
		return fmt.Sprintf("%s(find output)%s > ", colorYellow, colorReset)
	}
	return fmt.Sprintf("%s(find %d/%d)%s > ", colorYellow, m.findIdx+1, len(m.findMatches), colorReset)
}

// handleFindKey правит запрос и листает совпадения. Enter закрывает окно и
// оставляет подсветку, Esc снимает её совсем.
func (m tuiModel) handleFindKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEnter:
		m.state = tuiIdle
		m.input = ""
		m.cursorPos = 0
		return m, nil
	case tea.KeyEsc:
		m.state = tuiIdle
		m.input = ""
		m.cursorPos = 0
		m.clearFind()
		return m, nil
	case tea.KeyDown:
		m.findStep(1)
		return m, nil
	case tea.KeyUp:
		m.findStep(-1)
		return m, nil
	case tea.KeyF3:
		dir := 1
		if msg.Mod&tea.ModShift != 0 {
			dir = -1
		}
		m.findStep(dir)
		return m, nil
	case tea.KeyBackspace:
		m.deleteBackward()
		m.refreshFind()
		return m, nil
	case tea.KeyDelete:
		m.deleteForward()
		m.refreshFind()
		return m, nil
	case tea.KeyLeft:
		if m.cursorPos > 0 {
			m.cursorPos--
		}
		return m, nil
	case tea.KeyRight:
		if m.cursorPos < runeSliceLen(m.input) {
			m.cursorPos++
		}
		return m, nil
	case tea.KeyHome:
		m.cursorPos = 0
		return m, nil
	case tea.KeyEnd:
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	case tea.KeyPgUp:
		m.scrollBy(-(m.height - 3))
		return m, nil
	case tea.KeyPgDown:
		m.scrollBy(m.height - 3)
		return m, nil
	}
	switch msg.Keystroke() {
	case "ctrl+g", "ctrl+c":
		m.state = tuiIdle
		m.input = ""
		m.cursorPos = 0
		m.clearFind()
		return m, nil
	case "ctrl+n", "ctrl+f":
		m.findStep(1)
		return m, nil
	}
	if isInputKey(msg) {
		m.insertInput(msg.Text)
		m.refreshFind()
	}
	return m, nil
}

func (m *tuiModel) deleteBackward() {
	if m.cursorPos > 0 {
		r := []rune(m.input)
		m.input = string(append(r[:m.cursorPos-1], r[m.cursorPos:]...))
		m.cursorPos--
	}
}

func (m *tuiModel) deleteForward() {
	r := []rune(m.input)
	if m.cursorPos < len(r) {
		m.input = string(append(r[:m.cursorPos], r[m.cursorPos+1:]...))
	}
}

func (m tuiModel) handleEnter() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input)
	// В контексте "/" Enter исполняет выбранный пункт меню (стрелки/Tab),
	// а если совпадение единственное — завершает его автоматически.
	if strings.HasPrefix(m.input, "/") && !strings.ContainsAny(m.input, " \t") {
		if len(m.tabMatches) > 0 && m.tabIdx >= 0 {
			line = m.tabMatches[m.tabIdx]
		} else if mm := slashMatches(m.input); len(mm) == 1 {
			line = mm[0]
		}
	}
	m.input = ""
	m.cursorPos = 0
	m.tabMatches = nil
	m.tabIdx = -1
	if line == "" {
		// Режим терминала — это окно, а не отдельная оболочка поверх TUI.
		// Строка ввода у него появляется только если псевдотерминал не
		// запустился: тогда Enter пробует поднять терминал ещё раз.
		if m.rf.cfg.Mode == config.ModeShell && m.term == nil {
			return m.startTerminal()
		}
		return m, nil
	}
	// Повтор той же команды не добавляет новую запись: иначе стрелки
	// вверх/вниз на долгих сессиях упираются в хвост из одинаковых строк.
	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
	}
	m.histIdx = len(m.history)
	if strings.HasPrefix(line, "/") {
		return m.handleSlash(line)
	}
	if strings.HasPrefix(line, "!") {
		cmd := strings.TrimSpace(strings.TrimPrefix(line, "!"))
		m.addLine(fmt.Sprintf("%s$ %s%s", colorCyan, cmd, colorReset))
		return m.startDirectCommand(cmd)
	}
	// Режим терминала без псевдотерминала (ConPTY недоступен, оболочка не
	// найдена) не должен отправлять текст в модель: пользователь здесь ждёт
	// выполнения команды, а не её описания.
	if m.rf.cfg.Mode == config.ModeShell {
		if m.term == nil {
			return m.startTerminal()
		}
		return m, nil
	}
	// Новый запрос пользователя — новый диалог уточнений и новый лимит
	// автокоррекции.
	m.clarify = clarification{}
	m.fixTries = 0
	return m.submitToLLM(line)
}

func (m tuiModel) handleQuestionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEnter {
		answer := strings.TrimSpace(stripListMarker(m.input))
		m.input = ""
		m.cursorPos = 0
		if answer == "" {
			return m, nil
		}
		question := m.pendingResp.Question
		if m.clarify.exhausted() || m.clarify.repeatsLast(question) {
			m.state = tuiIdle
			m.addLine(fmt.Sprintf("%s(stopped: the model kept asking — rephrase the request)%s", colorGray, colorReset))
			return m, nil
		}
		followUp := m.clarify.followUp(m.s.lastRequest(), question, answer)
		m.state = tuiStreaming
		return m.submitToLLMEcho(followUp, answer)
	}
	if msg.String() == "ctrl+c" || msg.Code == tea.KeyEsc {
		m.state = tuiIdle
		m.input = ""
		m.cursorPos = 0
		m.addLine("(cancelled)")
		return m, nil
	}
	if msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h" {
		m.deleteBackward()
		return m, nil
	}
	if isInputKey(msg) {
		m.input = insertAt(m.input, m.cursorPos, msg.Text)
		m.cursorPos += runeSliceLen(msg.Text)
	}
	return m, nil
}

func (m tuiModel) handleHistoryPrev() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		m.addLine(fmt.Sprintf("%s(history is empty — nothing has been run yet)%s", colorYellow, colorReset))
		return m, nil
	}
	if m.histIdx > 0 {
		m.histIdx--
		m.input = m.history[m.histIdx]
		m.cursorPos = runeSliceLen(m.input)
	}
	return m, nil
}

func (m tuiModel) handleHistoryNext() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	if m.histIdx < len(m.history)-1 {
		m.histIdx++
		m.input = m.history[m.histIdx]
	} else {
		m.histIdx = len(m.history)
		m.input = ""
	}
	m.cursorPos = runeSliceLen(m.input)
	return m, nil
}

// isInputKey reports whether the key event represents printable text to insert:
// text may be multi-byte (Cyrillic etc.), so we gate on content, not byte
// length. Ctrl/alt combinations are commands, not text. AltGr (ctrl+alt
// together) still produces printable characters.
func isInputKey(msg tea.KeyPressMsg) bool {
	if msg.Text == "" || msg.Code == tea.KeyEnter {
		return false
	}
	mod := msg.Mod &^ (tea.ModShift | tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	if mod == tea.ModCtrl|tea.ModAlt {
		return true
	}
	return mod == 0
}

func insertAt(s string, pos int, substr string) string {
	runes := []rune(s)
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	out := make([]rune, 0, len(runes)+len([]rune(substr)))
	out = append(out, runes[:pos]...)
	out = append(out, []rune(substr)...)
	out = append(out, runes[pos:]...)
	return string(out)
}

func deleteWordBack(s string, pos *int) string {
	runes := []rune(s)
	if *pos <= 0 {
		return s
	}
	i := *pos - 1
	for i > 0 && runes[i] == ' ' {
		i--
	}
	for i > 0 && runes[i-1] != ' ' {
		i--
	}
	*pos = i
	return string(append(runes[:i], runes[*pos:]...))
}

func runeSliceLen(s string) int {
	return len([]rune(s))
}

// deleteWordBackPos возвращает позицию начала предыдущего слова (для alt+b).
func deleteWordBackPos(pos int, s string) int {
	runes := []rune(s)
	if pos <= 0 {
		return 0
	}
	i := pos - 1
	for i > 0 && runes[i] == ' ' {
		i--
	}
	for i > 0 && runes[i-1] != ' ' {
		i--
	}
	return i
}

// nextWordPos возвращает позицию начала следующего слова (для alt+f).
func nextWordPos(pos int, s string) int {
	runes := []rune(s)
	n := len(runes)
	i := pos
	for i < n && runes[i] != ' ' {
		i++
	}
	for i < n && runes[i] == ' ' {
		i++
	}
	return i
}

func deleteWordForward(s string, pos *int) string {
	runes := []rune(s)
	n := len(runes)
	if *pos >= n {
		return s
	}
	i := *pos
	for i < n && runes[i] != ' ' {
		i++
	}
	for i < n && runes[i] == ' ' {
		i++
	}
	out := string(append(runes[:*pos], runes[i:]...))
	return out
}

