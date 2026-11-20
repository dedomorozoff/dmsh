package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

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

