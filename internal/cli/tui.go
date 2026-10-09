package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/feedback"
	"github.com/dedomorozoff/dmsh/internal/policy"
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
	tuiRunning // команда выполняется: ввод свободен, виден спиннер
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
	pendingResp prompt.Response
	// clarify ведёт диалог уточнений текущего запроса: модель не должна
	// спрашивать одно и то же и не должна спрашивать бесконечно.
	clarify clarification

	searchQuery string
	searchIdx   int

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

	scrollOffset int

	// fixTries считает автокоррекции подряд. Без счётчика модель, которая
	// возвращает ту же падающую команду, запускает бесконечный цикл.
	fixTries int

	// running — фоновая команда (composer, тесты, сборка): пока она идёт,
	// ввод свободен, а состояние держит спиннер. cmdSeq нумерует запуски,
	// чтобы устаревший результат не затёр новый.
	running       bool
	cmdSeq        int
	cmdCancel     context.CancelFunc
	cmdCommand    string
	cmdStartedAt  time.Time
}

// slashCommands — commands offered by Tab completion and the "/" menu.
// Mode switches come first so they are visible in the truncated menu.
var slashCommands = []string{
	"/mode", "/shell", "/help", "/clear", "/exit", "/quit",
	"/history", "/cd", "/pwd", "/model", "/todo", "/stats", "/bind", "/alias", "/export", "/retry",
}

// slashDesc — one-line descriptions for the "/" menu.
var slashDesc = map[string]string{
	"/mode":    "show or switch mode",
	"/shell":   "open a real terminal session",
	"/help":    "show help for commands and modes",
	"/model":   "show the current model",
	"/todo":    "show the model's task list",
	"/stats":   "session statistics",
	"/export":  "export the last command",
	"/alias":   "manage aliases",
	"/history": "recent commands",
	"/cd":      "change directory",
	"/pwd":     "current directory",
	"/clear":   "clear screen",
	"/bind":    "show key bindings",
	"/retry":   "retry the last request",
	"/exit":    "exit",
	"/quit":    "exit",
}

// slashMatches возвращает команды, начинающиеся с prefix (точное совпадение
// исключается: выбирать нечего).
func slashMatches(prefix string) []string {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \t") {
		return nil
	}
	matches := make([]string, 0, len(slashCommands))
	for _, c := range slashCommands {
		if strings.HasPrefix(c, prefix) && c != prefix {
			matches = append(matches, c)
		}
	}
	return matches
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
	m := tuiModel{rf: rf, s: s, errW: io.Discard, modeLabel: string(rf.cfg.Mode), content: "", tabIdx: -1}
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
		return m, nil
	case tea.PasteMsg:
		if m.state == tuiIdle || m.state == tuiQuestion {
			m.input = insertAt(m.input, m.cursorPos, msg.Content)
			m.cursorPos += runeSliceLen(msg.Content)
			m.tabMatches = nil
		}
		return m, nil
	case tokenMsg:
		if len(msg.text) > 0 {
			m.streamed = true
			m.streamHasContent = true
			m.content = appendStream(m.content, msg.text)
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
	case shellExitedMsg:
		m.resumeFromShell(msg)
		return m, nil
	case terminalRanMsg:
		m.printTerminalRun(msg)
		return m, nil
	case cmdDoneMsg:
		return m.finishBackgroundCommand(msg)
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
		if msg.err != nil {
			if msg.err == context.Canceled {
				m.addLine("(cancelled)")
			} else {
				m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.err, colorReset))
			}
			return m, nil
		}
		if msg.downloaded {
			name := filepath.Base(msg.path)
			m.addLine(fmt.Sprintf("(downloaded %s)", name))
			return m.startModelLoad(name)
		}
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
	return m, nil
}

func (m tuiModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.SetContent(m.render())

	col := displayWidth(m.buildPrompt())
	switch m.state {
	case tuiConfirming:
		col = displayWidth(fmt.Sprintf("%sExecute? [y/N]: %s%s", colorYellow, m.confirmText, colorReset))
	case tuiSearch:
		col = displayWidth(m.searchPrompt()) + runeSliceLen(m.input)
	case tuiModelMenu:
		col = 0
	default:
		// Палитра рисует запрос без приглашения, поэтому не берём
		// buildPrompt() за основу.
		if m.state != tuiPalette {
			col = displayWidth(m.buildPrompt())
		}
		for _, r := range []rune(m.input)[:min(m.cursorPos, runeSliceLen(m.input))] {
			col += displayWidth(string(r))
		}
	}
	y := m.inputRow()
	switch m.state {
	case tuiModelMenu:
		// Menu rows replace the input line; place the cursor on the selected
		// entry (1 = the title row).
		y = m.inputRow() + 1 + m.modelIdx
		if m.height > 1 && y > m.height-2 {
			y = m.height - 2
		}
	case tuiConfirming, tuiSearch:
		// Confirming/search rows live inside the scrollback; step back to the
		// wrapped block that contains the current cursor row.
		line := ""
		if m.state == tuiConfirming {
			line = fmt.Sprintf("%sExecute? [y/N]: %s%s", colorYellow, m.confirmText, colorReset)
		} else {
			line = m.searchPrompt() + colorGreen + m.input
		}
		y -= len(hardWrap(line, m.width))
		if y < 0 {
			y = 0
		}
	}
	if m.width > 0 {
		y += col / m.width
		col = col % m.width
	}

	v.Cursor = &tea.Cursor{Position: tea.Position{X: col, Y: y}, Shape: tea.CursorBlock, Blink: true}
	if m.state == tuiStreaming {
		v.Cursor = nil
	}
	return v
}

// hardWrap splits a display string into rows each no wider than width
// (0 or negative = no wrapping). ANSI sequences do not count toward width.
func hardWrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	return strings.Split(ansi.Hardwrap(s, width, true), "\n")
}

// fitWidth truncates a display string so it fits the given column width.
func fitWidth(s string, width int) string {
	if width <= 0 || displayWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…") + reset
}

// layoutRows builds the scrollable body rows and the input display rows for
// the current width, wrapping any row wider than the terminal. Without this
// the terminal soft-wraps long lines, so the frame no longer matches the
// screen rows: the status line breaks apart and the render falls into garbage.
func (m tuiModel) layoutRows() (bodyRows, inRows []string) {
	var body strings.Builder
	if m.content != "" {
		body.WriteString(m.content)
		if !strings.HasSuffix(m.content, "\n") {
			body.WriteString("\n")
		}
	}
	switch m.state {
	case tuiConfirming:
		fmt.Fprintf(&body, "%sExecute? [y/N]: %s%s\n", colorYellow, m.confirmText, colorReset)
	case tuiSearch:
		body.WriteString(m.searchPrompt() + colorGreen + m.input + "\n")
	}

	raw := strings.Split(body.String(), "\n")
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	for _, l := range raw {
		bodyRows = append(bodyRows, hardWrap(l, m.width)...)
	}

	switch m.state {
	case tuiModelMenu:
		inRows = m.menuRows()
	case tuiPalette:
		// Палитра заменяет строку ввода: запрос уже отфильтрован.
		inRows = m.paletteRows()
	case tuiConfirming, tuiSearch:
		// Обе строки живут в скроллбеке, отдельной строки ввода нет.
	default:
		inRows = hardWrap(m.buildPrompt()+m.input, m.width)
		if m.streaming {
			inRows = hardWrap(fmt.Sprintf("%s[dmsh] thinking…%s", colorCyan, colorReset), m.width)
		} else if m.running {
			// Команда выполняется в фоне: ввод свободен, виден прогресс.
			inRows = append(inRows, hardWrap(fmt.Sprintf("%s… running %s (Esc cancels)%s",
				colorYellow, shortCmd(m.cmdCommand), colorReset), m.width)...)
		}
		// Slash-command menu with descriptions while typing "/".
		const maxVisible = 8
		menu := m.menuList()
		for i, cmd := range menu {
			if i >= maxVisible {
				if rest := len(menu) - maxVisible; rest > 0 {
					inRows = append(inRows, colorGray+fmt.Sprintf("(+%d more)", rest)+colorReset)
				}
				break
			}
			marker, style := "  ", colorGray
			descStyle := colorGray
			if i == m.tabIdx {
				marker, style = "> ", colorYellow+colorBold
				descStyle = ""
			}
			item := fmt.Sprintf("%s%s%s%s %s— %s%s", style, marker, cmd, colorReset, descStyle, slashDesc[cmd], colorReset)
			inRows = append(inRows, hardWrap(fitWidth(item, m.width), m.width)...)
		}
	}
	return bodyRows, inRows
}

// shortCmd укорачивает команду для строки состояния: длинные composer-строки
// иначе разваливают вёрстку.
func shortCmd(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len([]rune(cmd)) <= 60 {
		return cmd
	}
	return string([]rune(cmd)[:57]) + "…"
}

// inputRow returns the row (0-based) where the input line starts inside the
// rendered frame, after scrollback truncation keeps the status line pinned to
// the terminal bottom.
func (m tuiModel) inputRow() int {
	bodyRows, inRows := m.layoutRows()
	if m.height <= 0 {
		return len(bodyRows)
	}
	avail := m.height - 1 - len(inRows)
	if avail < 0 {
		avail = 0
	}
	if len(bodyRows) > avail {
		return avail
	}
	return len(bodyRows)
}

func (m tuiModel) render() string {
	bodyLines, inLines := m.layoutRows()
	status := fitWidth(m.statusline(), m.width)

	if m.height <= 0 {
		return strings.Join(append(bodyLines, inLines...), "\n")
	}

	avail := m.height - 1 - len(inLines)
	if avail < 0 {
		avail = 0
	}

	total := len(bodyLines)
	maxOffset := total - avail
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	start := total - avail - m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + avail
	if end > total {
		end = total
	}
	visible := bodyLines[start:end]

	frame := make([]string, 0, m.height)
	frame = append(frame, visible...)
	frame = append(frame, inLines...)
	for len(frame) < m.height-1 {
		frame = append(frame, "")
	}
	frame = append(frame, status)
	return strings.Join(frame, "\n")
}

func (m tuiModel) statusline() string {
	short, _ := os.Getwd()
	shortName := shortPath(short)
	left := fmt.Sprintf("%s[%s]%s mode:%s%s%s  model:%s%s",
		colorCyan, shortName, colorReset,
		colorYellow, m.modeLabel, colorReset,
		gray, m.modelLabel())
	right := fmt.Sprintf("%shelp%s %sF1%s   %spalette%s %sCtrl+P%s",
		colorGray, colorReset, colorGray, colorReset,
		colorGray, colorReset, colorGray, colorReset)
	ls := displayWidth(left)
	rs := displayWidth(right)
	pad := 0
	if m.width > ls+rs {
		pad = m.width - ls - rs
	}
	sep := strings.Repeat(" ", pad)
	if pad < 1 {
		sep = " "
	}
	return left + sep + right
}

// modelLabel показывает удалённую модель, когда активен pollinations, иначе
// имя локального файла. Пустая строка на старте — это "none".
func (m tuiModel) modelLabel() string {
	if m.s != nil && m.s.provider == config.ProviderPollinations {
		model := m.s.cfg.RemoteModel
		if model == "" {
			model = config.DefaultRemoteModel
		}
		return "pollinations:" + model
	}
	modelPath := ""
	if m.s != nil {
		modelPath = m.s.cfg.ModelPath
	}
	if modelPath == "" {
		modelPath = m.rf.cfg.ModelPath
	}
	return shortModelName(modelPath)
}

func shortModelName(p string) string {
	if p == "" {
		return "none"
	}
	base := p
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	if len(base) > 24 {
		base = "…" + base[len(base)-23:]
	}
	return base
}

func (m tuiModel) buildPrompt() string {
	cwd, _ := os.Getwd()
	short := shortPath(cwd)
	return fmt.Sprintf("%s[%s]%s > %s", colorGray, short, colorReset, colorReset)
}

func (m *tuiModel) addLine(text string) {
	m.content = appendLine(m.content, text)
}

func (m *tuiModel) clearContent() {
	m.content = ""
	m.scrollOffset = 0
}

func (m *tuiModel) scrollUp() {
	m.scrollOffset++
}

func (m *tuiModel) scrollDown() {
	if m.scrollOffset > 0 {
		m.scrollOffset--
	}
}

func (m *tuiModel) scrollBy(delta int) {
	m.scrollOffset += delta
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func appendLine(content, text string) string {
	if !strings.HasSuffix(content, "\n") && content != "" {
		content += "\n"
	}
	return content + text + "\n"
}

func appendStream(content, text string) string {
	content += text
	return content
}

func displayWidth(s string) int {
	return ansi.StringWidth(s)
}

func (m tuiModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Глобальные биндинги: работают в любом состоянии, включая меню,
	// поиск, подтверждение и стриминг. Иначе подсказки в help и статусной
	// строке врали бы в половине экранов.
	// F1 матчится и по коду, и по строке: часть терминалов присылает
	// shift+f1. Палитра не исключается — help нужен из любого экрана.
	if msg.Code == tea.KeyF1 || msg.Keystroke() == "f1" {
		m.showHelp()
		return m, nil
	}
	if msg.Keystroke() == "ctrl+q" {
		return m.quitTui()
	}
	if m.state == tuiPalette {
		return m.handlePaletteKey(msg)
	}
	if m.state == tuiSearch {
		return m.handleSearchKey(msg)
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
	case tea.KeyTab:
		// Shift+Tab приходит как KeyTab с модификатором Shift: он занят
		// под переключение режима, обычный Tab остаётся комплитом.
		if msg.Mod&tea.ModShift != 0 {
			m.cycleMode()
			return m, nil
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
		if m.cursorPos < runeSliceLen(m.input) {
			m.cursorPos++
		}
	case "ctrl+n":
		if m.menuSelect(1) {
			return m, nil
		}
		return m.handleHistoryNext()
	case "ctrl+o":
		// Model selection / download menu.
		return m.openModelMenu(), nil
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
	case "ctrl+p":
		return m.openPalette(), nil
	}
	return m, nil
}

// cycleMode переключает режим по кругу и печатает новый. Тот же путь, что
// и у /mode: конфиг оболочки, конфиг сессии и подпись в статусной строке
// должны разойтись не могут.
func (m *tuiModel) cycleMode() {
	next := nextMode(m.rf.cfg.Mode)
	m.rf.cfg.Mode = next
	m.s.cfg.Mode = next
	m.modeLabel = modeLabel(next)
	m.addLine(fmt.Sprintf("%s[dmsh] mode: %s%s", colorCyan, modeLabel(next), colorReset))
	if next == config.ModeShell {
		m.addLine(fmt.Sprintf("%spress Enter on an empty line to open a terminal%s", colorGray, colorReset))
	}
	if next == config.ModeHelp {
		m.addLine(fmt.Sprintf("%srun_command is disabled in help mode%s", colorGray, colorReset))
	}
}

// quitTui останавливает стриминг и завершает программу. Отмена контекста
// нужна, чтобы не оставить инференс работать после выхода.
func (m tuiModel) quitTui() (tea.Model, tea.Cmd) {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.streaming = false
	m.state = tuiIdle
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
		// В режиме терминала пустая строка — не «ничего», а открытие
		// настоящей оболочки с отданным терминалом.
		if m.rf.cfg.Mode == config.ModeShell {
			return m.openTerminal()
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
	if m.rf.cfg.Mode == config.ModeShell {
		// Непустая строка выполняется в псевдотерминале: программам, которым
		// нужен tty (цвета, интерактив, размер окна), достаётся настоящий
		// терминал, а TUI остаётся на экране.
		m.addLine(fmt.Sprintf("%s$ %s%s", colorCyan, line, colorReset))
		return m.startTerminalCommand(line)
	}
	// Новый запрос пользователя — новый диалог уточнений и новый лимит
	// автокоррекции.
	m.clarify = clarification{}
	m.fixTries = 0
	return m.submitToLLM(line)
}

func (m tuiModel) handleSlash(line string) (tea.Model, tea.Cmd) {
	switch {
	case line == "/exit", line == "/quit", line == "exit", line == "quit":
		m.addLine("bye!")
		return m, tea.Quit
	case line == "/help", line == "help":
		m.showHelp()
	case strings.HasPrefix(line, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(line, "/cd "))
		if err := os.Chdir(target); err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
	case line == "/cd":
		if home, err := os.UserHomeDir(); err == nil {
			if err := os.Chdir(home); err != nil {
				m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
			}
		}
	case line == "/shell", line == "terminal":
		if m.rf.cfg.Mode != config.ModeShell {
			// /shell вне режима терминала всё равно открывает сессию, но
			// режим должен ей соответствовать, иначе следующий ввод снова
			// уйдёт в модель.
			m.rf.cfg.Mode = config.ModeShell
			m.s.cfg.Mode = config.ModeShell
			m.modeLabel = modeLabel(config.ModeShell)
		}
		return m.openTerminal()
	case line == "/clear", line == "clear":
		m.clearContent()
		m.addLine("")
	case line == "/pwd", line == "pwd":
		wd, _ := os.Getwd()
		m.addLine(wd)
	case line == "/history", line == "history":
		m.showHistory()
	case line == "/bind", line == "/bind keys":
		m.showKeyBindings()
	case line == "/stats":
		m.showStats()
	case line == "/model":
		m.showModel()
	case line == "/todo", line == "todo":
		m.showTodo()
	case strings.HasPrefix(line, "/todo "):
		handleTodo(strings.TrimSpace(strings.TrimPrefix(line, "/todo ")), io.Discard, m.s)
		m.showTodo()
	case line == "/retry":
		lastInput := m.s.lastRequest()
		if lastInput == "" {
			m.addLine(fmt.Sprintf("%sNo previous request to retry.%s", colorYellow, colorReset))
			return m, nil
		}
		m.clarify = clarification{}
		return m.submitToLLM("Alternative approach needed. Previous attempt failed. " + lastInput)
	case strings.HasPrefix(line, "/export"):
		m.handleExport(line)
	case strings.HasPrefix(line, "/alias"):
		m.handleAlias(line)
	case IsModeCommand(line):
		if line == "/mode" {
			m.addLine(fmt.Sprintf("Current mode: %s", modeLabel(m.rf.cfg.Mode)))
			return m, nil
		}
		if newMode := ParseModeCommand(line); newMode != "" {
			m.rf.cfg.Mode = newMode
			m.s.cfg.Mode = newMode
			m.modeLabel = modeLabel(newMode)
		}
	default:
		m.addLine(fmt.Sprintf("%sunknown command: %s%s", colorRed, line, colorReset))
	}
	return m, nil
}

func (m tuiModel) submitToLLM(input string) (tea.Model, tea.Cmd) {
	return m.submitToLLMEcho(input, input)
}

// submitToLLMEcho отправляет модели send, но в транскрипт печатает echo.
// Для уточнений это разные строки: модели уходит вопрос с ответом, а
// пользователю — только его ответ.
func (m tuiModel) submitToLLMEcho(send, echo string) (tea.Model, tea.Cmd) {
	// Echo the request into the transcript so it does not vanish when the
	// input line is cleared on enter.
	m.addLine(fmt.Sprintf("%s> %s%s", colorGray, echo, colorReset))
	mm, cmd := m.startLLMStream("run", send)
	return mm, cmd
}

// startLLMStream запускает фоновый стриминг LLM и возвращает команду,
// которая доставляет токены и результат в Update. Каналы НЕ закрываются,
// чтобы avoid busy-loop на закрытом канале; завершение сигналится через done.
func (m tuiModel) startLLMStream(mode, input string) (tuiModel, tea.Cmd) {
	m.streamCtx, m.streamCancel = context.WithCancel(context.Background())
	tokenCh := make(chan tokenMsg, 256)
	doneCh := make(chan streamResult, 1)
	m.tokenCh = tokenCh
	m.streamDone = doneCh
	m.streaming = true
	m.streamed = false
	m.streamHasContent = false
	m.state = tuiStreaming

	var buf strings.Builder
	w := &tuiWriter{buf: &buf, tokenCh: tokenCh}

	go func() {
		resp, raw, err := m.s.askStream(m.streamCtx, mode, input, w)
		if buf.Len() > 0 {
			select {
			case tokenCh <- tokenMsg{text: buf.String()}:
			default:
			}
			buf.Reset()
		}
		doneCh <- streamResult{resp: resp, raw: raw, err: err}
	}()

	return m, waitForToken(tokenCh, doneCh)
}

func (m tuiModel) executeResponse(resp prompt.Response) (tuiModel, tea.Cmd) {
	if m.rf.cfg.DryRun {
		m.addLine("(dry-run: command not executed)")
		return m, nil
	}
	if m.rf.cfg.Mode == config.ModeHelp {
		m.addLine(fmt.Sprintf("\n%s=== Ready Command ===%s", colorBold+colorGreen, colorReset))
		m.addLine(fmt.Sprintf("$ %s", resp.Command))
		if resp.Explanation != "" {
			m.addLine(fmt.Sprintf("\n%sExplanation:%s %s", colorBold+colorYellow, colorReset, resp.Explanation))
		}
		m.addLine(fmt.Sprintf("%sCopy the command or prefix with ! to execute immediately%s", colorGray, colorReset))
		return m, nil
	}
	dec := evaluatePolicy(resp, &m.rf.cfg)
	if !dec.Allowed {
		m.addLine("(command blocked by security policy)")
		return m, nil
	}
	if dec.Risk != prompt.RiskLow || resp.NeedsConfirmation {
		m.state = tuiConfirming
		m.pendingResp = resp
		m.confirmText = ""
		m.confirmOK = nil
		return m, nil
	}
	return m.runCommand(resp)
}

func (m tuiModel) runCommand(resp prompt.Response) (tuiModel, tea.Cmd) {
	m.addLine(fmt.Sprintf("\n%s$ %s%s", colorCyan, resp.Command, colorReset))
	m.addLine(fmt.Sprintf("%s%s%s", gray, strings.Repeat("─", 40), colorReset))
	if handled, shouldExit, err := runBuiltin(resp.Command, io.Discard, io.Discard, m.s.recent); handled {
		if err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
		if shouldExit {
			m.addLine("bye!")
		}
		m.s.addRecentAndHistory(resp.Command, "llm")
		return m, nil
	}
	// Долгие команды (composer, тесты, сборка) идут в фоне: TUI остаётся
	// отзывчивым, вывод появится по завершении. Отмена — Esc/Ctrl+C.
	return m.startBackgroundCommand(resp)
}

// startBackgroundCommand запускает команду LLM в фоне через tea.Cmd.
// Синхронного executor.Run здесь больше нет: раньше Update висел на нём,
// и весь интерфейс замирал до конца команды.
func (m tuiModel) startBackgroundCommand(resp prompt.Response) (tuiModel, tea.Cmd) {
	m.cancelCommand()
	ctx, cancel := context.WithCancel(context.Background())
	m.cmdCancel = cancel
	m.cmdSeq++
	seq := m.cmdSeq
	m.cmdCommand = resp.Command
	m.cmdStartedAt = time.Now()
	m.running = true
	m.state = tuiRunning
	dec := evaluatePolicy(resp, &m.rf.cfg)
	lite := policyDecisionLite{allowed: dec.Allowed, risk: dec.Risk, reason: dec.Reason}
	shell := m.rf.cfg.Shell
	command := resp.Command
	m.addLine(fmt.Sprintf("%srunning in background — Esc cancels%s", colorGray, colorReset))
	return m, func() tea.Msg {
		res := executor.Run(ctx, shell, command)
		return cmdDoneMsg{seq: seq, resp: resp, res: res, dec: lite}
	}
}

// cancelCommand останавливает фоновую команду, если она идёт.
func (m *tuiModel) cancelCommand() {
	if m.cmdCancel != nil {
		m.cmdCancel()
		m.cmdCancel = nil
	}
	m.running = false
}

// finishBackgroundCommand печатает результат фоновой команды и ведёт
// историю/аудит тем же путём, что и синхронный запуск.
func (m tuiModel) finishBackgroundCommand(msg cmdDoneMsg) (tuiModel, tea.Cmd) {
	if msg.seq != m.cmdSeq {
		return m, nil
	}
	m.cancelCommand()
	m.state = tuiIdle
	elapsed := time.Since(m.cmdStartedAt).Round(time.Second)
	m.s.addRecentAndHistory(msg.resp.Command, "llm")
	m.s.audit(msg.resp.Command, "llm", restoreDecision(msg.dec), msg.res)
	if msg.res.Stdout != "" {
		m.addLine(msg.res.Stdout)
	}
	if msg.res.Stderr != "" {
		m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.res.Stderr, colorReset))
	}
	if msg.res.Err != nil && msg.res.ExitCode != 0 {
		m.addLine(fmt.Sprintf("%sexit %d after %s%s", colorGray, msg.res.ExitCode, elapsed, colorReset))
	} else {
		m.addLine(fmt.Sprintf("%sdone in %s%s", colorGray, elapsed, colorReset))
	}
	fb := feedback.Analyze(msg.resp.Command, msg.res.Stdout, msg.res.Stderr, msg.res.ExitCode)
	if fb.Success {
		// Команда удалась — цикл автокоррекции закончен, лимит снова полон.
		m.fixTries = 0
		if hint := fb.Format(); hint != "" {
			m.addLine(fmt.Sprintf("\n%s[dmsh]%s %s%s%s", colorGreen, colorReset, colorGreen, hint, colorReset))
		}
		return m, nil
	}
	return m.autoCorrect(msg.resp, msg.res)
}

// restoreDecision возвращает полное решение политики из лёгкой копии:
// нужно для аудита фоновой команды после её завершения.
func restoreDecision(lite policyDecisionLite) policy.Decision {
	return policy.Decision{Allowed: lite.allowed, Risk: lite.risk, Reason: lite.reason}
}

// maxAutoFix ограничивает автокоррекцию подряд. Модель может сколько угодно
// возвращать неработающую команду; без предела каждая попытка — это новый
// запрос к модели и новый запуск команды, то есть бесконечный цикл.
const maxAutoFix = 2

// autoCorrect просит модель исправить упавшую команду. Число попыток
// ограничено: исчерпав лимит, останавливаемся и показываем, что делать
// дальше, вместо того чтобы крутиться.
func (m tuiModel) autoCorrect(resp prompt.Response, res executor.Result) (tuiModel, tea.Cmd) {
	stderr := res.Stderr
	if stderr == "" && res.Err != nil {
		stderr = res.Err.Error()
	}
	m.fixTries++
	if m.fixTries > maxAutoFix {
		m.fixTries = 0
		m.state = tuiIdle
		m.addLine(fmt.Sprintf("%s[dmsh]%s the command failed %d times, giving up on auto-fix", colorRed, colorReset, maxAutoFix))
		if stderr != "" {
			m.addLine(fmt.Sprintf("%s%s%s", colorGray, stderr, colorReset))
		}
		m.addLine(fmt.Sprintf("%srun the command yourself or rephrase the request%s", colorGray, colorReset))
		return m, nil
	}

	m.addLine(fmt.Sprintf("\n%s[dmsh]%s Error detected (code %d). Requesting auto-correction from LLM (attempt %d/%d)...%s",
		colorYellow, colorReset, res.ExitCode, m.fixTries, maxAutoFix, colorReset))
	m.addLine(fmt.Sprintf("%s%s%s", gray, strings.Repeat("─", 40), colorReset))
	m.s.addFixNote()
	correctionInput := fmt.Sprintf("Command '%s' failed.\nExit code: %d\nStderr:\n%s\n\nPlease fix the command so it runs successfully on the current OS.", resp.Command, res.ExitCode, stderr)
	// Автокоррекция — тоже новый запрос, уточнения сбрасываются.
	m.clarify = clarification{}
	return m.startLLMStream("run", correctionInput)
}

func (m tuiModel) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmed := false
	switch msg.String() {
	case "y", "Y", "yes":
		confirmed = true
		m.state = tuiIdle
	case "n", "N", "no", "enter":
		confirmed = false
		m.state = tuiIdle
	case "ctrl+c", "esc":
		confirmed = false
		m.state = tuiIdle
		m.input = ""
		m.cursorPos = 0
	default:
		return m, nil
	}
	if m.confirmOK != nil {
		m.confirmOK(confirmed)
		m.confirmOK = nil
	}
	if confirmed && m.pendingResp.Intent == prompt.IntentRunCommand {
		return m.runCommand(m.pendingResp)
	}
	if !confirmed {
		m.addLine("(cancelled)")
	}
	return m, nil
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

func (m *tuiModel) executeDirect(cmd string) {
	if handled, shouldExit, err := runBuiltin(cmd, io.Discard, io.Discard, m.s.recent); handled {
		if err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
		if shouldExit {
			m.addLine("bye!")
		}
		m.s.addRecentAndHistory(cmd, "direct")
		return
	}
	// Прямая команда тоже идёт в фоне: иначе composer через "!" вешает ввод.
	mm, _ := m.startDirectCommand(cmd)
	*m = mm
}

// startDirectCommand запускает "!"-команду в фоне. Встроенные (cd, exit)
// выполняются сразу — им фон не нужен.
func (m tuiModel) startDirectCommand(cmd string) (tuiModel, tea.Cmd) {
	if handled, shouldExit, err := runBuiltin(cmd, io.Discard, io.Discard, m.s.recent); handled {
		if err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
		if shouldExit {
			m.addLine("bye!")
		}
		m.s.addRecentAndHistory(cmd, "direct")
		return m, nil
	}
	m.cancelCommand()
	ctx, cancel := context.WithCancel(context.Background())
	m.cmdCancel = cancel
	m.cmdSeq++
	seq := m.cmdSeq
	m.cmdCommand = cmd
	m.cmdStartedAt = time.Now()
	m.running = true
	m.state = tuiRunning
	shell := m.rf.cfg.Shell
	m.addLine(fmt.Sprintf("%srunning in background — Esc cancels%s", colorGray, colorReset))
	return m, func() tea.Msg {
		// Прямые команды интерактивные: вывод идёт пользователю напрямую.
		res := executor.Run(ctx, shell, cmd)
		return directDoneMsg{seq: seq, cmd: cmd, res: res}
	}
}

// finishDirectCommand печатает результат фоновой "!"-команды.
func (m tuiModel) finishDirectCommand(msg directDoneMsg) (tuiModel, tea.Cmd) {
	if msg.seq != m.cmdSeq {
		return m, nil
	}
	m.cancelCommand()
	m.state = tuiIdle
	elapsed := time.Since(m.cmdStartedAt).Round(time.Second)
	m.s.addRecentAndHistory(msg.cmd, "direct")
	m.s.audit(msg.cmd, "direct", directDecision(), msg.res)
	if msg.res.Stdout != "" {
		m.addLine(msg.res.Stdout)
	}
	if msg.res.Stderr != "" {
		m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.res.Stderr, colorReset))
	}
	if msg.res.Err != nil {
		m.addLine(fmt.Sprintf("%sexit %d: %v%s", colorRed, msg.res.ExitCode, msg.res.Err, colorReset))
	} else {
		m.addLine(fmt.Sprintf("%sdone in %s%s", colorGray, elapsed, colorReset))
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

func waitForToken(ch <-chan tokenMsg, done <-chan streamResult) tea.Cmd {
	return func() tea.Msg {
		select {
		case tok := <-ch:
			return tok
		case res := <-done:
			return streamDoneMsg{resp: res.resp, err: res.err}
		}
	}
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

func (m *tuiModel) showHelp() {
	m.addLine(fmt.Sprintf("%s=== dmsh help ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("%sModes:%s", colorBold, colorReset))
	m.addLine(fmt.Sprintf("  %sai%s     — generate a command and run it automatically", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %shelp%s   — command plus explanation, nothing is executed", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sterminal%s— a real shell session with the terminal handed over", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sShift+Tab%s — cycle modes, %sCtrl+P%s — pick one in the palette", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/mode%s [ai|help|shell] — show or set the mode", colorYellow, colorReset))
	m.addLine("")
	m.addLine(fmt.Sprintf("%sCommands:%s", colorBold, colorReset))
	m.addLine(fmt.Sprintf("  %s!cmd%s         — run shell command directly", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/cd%s path     — change directory", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/pwd%s         — current directory", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/history%s     — recent commands", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/stats%s       — session statistics", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/model%s       — current model", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/shell%s       — open a terminal session", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/bind%s        — key bindings", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/help%s        — this info", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/exit%s        — exit (or Ctrl+Q)", colorYellow, colorReset))
}

func (m *tuiModel) showKeyBindings() {
	m.addLine(fmt.Sprintf("%s=== Key Bindings ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("  %sF1%s / %s/help%s    — this info", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+P%s          — command palette (all commands and modes)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+Q%s          — exit", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sEsc%s / %sCtrl+C%s — cancel / stop", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+A/E/U/K%s   — start/end/delete-to-start/delete-to-end", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+R/S%s       — history search", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+N%s          — next history", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sAlt+B/F/D%s      — word back/forward/delete", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+W%s         — delete word back", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+L%s         — clear screen", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+O%s         — model menu (install / switch)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sTab%s            — complete slash command", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sShift+Tab%s      — cycle modes (ai → help → terminal)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s↑/↓%s            — previous / next command from history", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sPgUp/PgDn%s      — scroll output", colorYellow, colorReset))
}

// showHistory печатает историю из файла, а не из s.recent: recent хранит
// только последние команды текущей сессии и обрезается до 10 записей, так
// что после перезапуска /history показывал бы почти пустоту.
func (m *tuiModel) showHistory() {
	entries := m.s.historyEntries()
	if len(entries) == 0 {
		m.addLine(fmt.Sprintf("%sno history yet%s", colorGray, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("%s=== history (%d) ===%s", colorBold+colorCyan, len(entries), colorReset))
	for i, e := range entries {
		ts := e.Timestamp.Format("01-02 15:04")
		m.addLine(fmt.Sprintf("%s%4d%s  %s%s%s  %s",
			colorGray, i+1, colorReset,
			colorGray, ts, colorReset,
			e.Command))
	}
}

func (m *tuiModel) showStats() {
	elapsed := time.Since(m.s.stats.StartTime).Round(time.Second)
	total := m.s.stats.CommandsLLM + m.s.stats.CommandsDirect
	m.addLine(fmt.Sprintf("\n%s=== Session Stats ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("  %sStarted:%s       %s ago", colorBold, colorReset, elapsed))
	m.addLine(fmt.Sprintf("  %sRequests:%s      %d", colorBold, colorReset, m.s.stats.Requests))
	m.addLine(fmt.Sprintf("  %sCommands run:%s  %d (LLM: %d, direct: %d)", colorBold, colorReset, total, m.s.stats.CommandsLLM, m.s.stats.CommandsDirect))
	m.addLine(fmt.Sprintf("  %sErrors fixed:%s  %d", colorBold, colorReset, m.s.stats.ErrorsFix))
	m.addLine(fmt.Sprintf("  %sCurrent mode:%s  %s\n", colorBold, colorReset, modeLabel(m.rf.cfg.Mode)))
}

func (m *tuiModel) showModel() {
	m.addLine(fmt.Sprintf("\n%s=== Model ===%s", colorBold+colorCyan, colorReset))

	provider := m.s.provider
	if provider == "" {
		provider = m.rf.cfg.Provider
	}
	if provider == "" {
		provider = config.ProviderAuto
	}
	m.addLine(fmt.Sprintf("  %sProvider:%s  %s", colorBold, colorReset, provider))

	if provider == config.ProviderPollinations {
		m.addLine(fmt.Sprintf("  %sIn use:%s  %s", colorBold, colorReset, firstNonEmpty(m.s.cfg.RemoteModel, config.DefaultRemoteModel)))
		m.addLine(fmt.Sprintf("%sEndpoint:%s %s", colorGray, colorReset, pollinationsEndpoint(m.s.cfg.RemoteBaseURL)))
		m.addLine(fmt.Sprintf("  %sSearch:%s   %s", colorBold, colorReset, firstNonEmpty(m.s.cfg.SearchModel, config.DefaultSearchModel)))
		m.addLine(fmt.Sprintf("  %sTools:%s    %s", colorBold, colorReset, toolsLabel(m.s.cfg.ToolsEnabled)))
		m.addLine(fmt.Sprintf("%sAuth:%s     anonymous (no token)", colorGray, colorReset))
		m.addLine("")
		return
	}

	modelPath := m.s.cfg.ModelPath
	if modelPath == "" {
		modelPath = m.rf.cfg.ModelPath
	}
	if modelPath == "" {
		m.addLine(fmt.Sprintf("  %snone%s\n", colorYellow, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("  %sIn use:%s  %s", colorBold, colorReset, modelPath))
	if fi, err := os.Stat(modelPath); err == nil {
		m.addLine(fmt.Sprintf("  %sSize:%s    %d MB", colorBold, colorReset, fi.Size()/1024/1024))
	}
	m.addLine("")
}

func (m *tuiModel) showTodo() {
	if m.s.todos == nil {
		m.addLine("todo list is not available")
		return
	}
	items := m.s.todos.Items()
	if len(items) == 0 {
		m.addLine(fmt.Sprintf("%stodo list is empty.%s", colorGray, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("\n%s=== todo (%d open) ===%s", colorBold+colorCyan, m.s.todos.Open(), colorReset))
	for _, it := range items {
		if it.Done {
			m.addLine(fmt.Sprintf("  %s[%d] %s (done)%s", colorGray, it.ID, it.Task, colorReset))
			continue
		}
		m.addLine(fmt.Sprintf("  %s[%d]%s %s", colorYellow, it.ID, colorReset, it.Task))
	}
	m.addLine("")
}

// firstNonEmpty возвращает первое непустое значение — для настроек,
// которые могли остаться пустыми в конфиге.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *tuiModel) handleExport(line string) {
	if len(m.s.recent) == 0 {
		m.addLine(fmt.Sprintf("%sNo commands to export yet.%s", colorYellow, colorReset))
		return
	}
	lastCmd := m.s.recent[len(m.s.recent)-1]
	parts := strings.Fields(line)
	if len(parts) >= 3 && parts[1] == "last" && parts[2] == ">" && len(parts) >= 4 {
		filePath := strings.Join(parts[3:], " ")
		if err := os.WriteFile(filePath, []byte(lastCmd+"\n"), 0644); err != nil {
			m.addLine(fmt.Sprintf("%scould not write file: %v%s", colorRed, err, colorReset))
			return
		}
		m.addLine(fmt.Sprintf("%s✓ Written to %s%s", colorGreen, filePath, colorReset))
		return
	}
	if err := copyToClipboard(lastCmd); err != nil {
		m.addLine(fmt.Sprintf("%sClipboard unavailable: %v%s", colorYellow, err, colorReset))
		m.addLine(fmt.Sprintf("%sLast command: %s%s%s", colorGray, colorCyan, lastCmd, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("%s✓ Copied to clipboard:%s %s%s%s", colorGreen, colorReset, colorCyan, lastCmd, colorReset))
}

func (m *tuiModel) handleAlias(line string) {
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/alias"))
	cfg := &m.rf.cfg
	if arg == "" {
		if len(cfg.Aliases) == 0 {
			m.addLine(fmt.Sprintf("%sNo aliases defined. Use /alias name=\"request\"%s", colorGray, colorReset))
			return
		}
		m.addLine(fmt.Sprintf("\n%s=== Aliases ===%s", colorBold+colorCyan, colorReset))
		for k, v := range cfg.Aliases {
			m.addLine(fmt.Sprintf("  %s%s%s = %s%s%s", colorYellow, k, colorReset, colorGray, v, colorReset))
		}
		m.addLine("")
		return
	}
	if strings.HasPrefix(arg, "-d ") {
		name := strings.TrimSpace(arg[3:])
		if _, ok := cfg.Aliases[name]; !ok {
			m.addLine(fmt.Sprintf("%salias %q not found%s", colorRed, name, colorReset))
			return
		}
		delete(cfg.Aliases, name)
		_ = config.Save(*cfg)
		m.addLine(fmt.Sprintf("%s✓ Alias %q removed%s", colorGreen, name, colorReset))
		return
	}
	eqIdx := strings.IndexByte(arg, '=')
	if eqIdx == -1 {
		m.addLine(fmt.Sprintf("%sUsage: /alias name=\"request\" or /alias -d name%s", colorYellow, colorReset))
		return
	}
	name := strings.TrimSpace(arg[:eqIdx])
	val := strings.Trim(strings.TrimSpace(arg[eqIdx+1:]), `"`)
	if name == "" || val == "" {
		m.addLine(fmt.Sprintf("%sinvalid alias: name and value must not be empty%s", colorRed, colorReset))
		return
	}
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	cfg.Aliases[name] = val
	_ = config.Save(*cfg)
	m.addLine(fmt.Sprintf("%s✓ Alias %q = %q saved%s", colorGreen, name, val, colorReset))
}
