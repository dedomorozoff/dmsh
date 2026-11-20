package cli

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/vt"
)

func (m tuiModel) View() tea.View {
	return m.renderView()
}

// renderView собирает кадр вместе с позицией курсора. Модальные окна
// позиционируются по собственным координатам внутри рамки, поэтому для них
// раскладку строки ввода считать не нужно.
func (m tuiModel) renderView() tea.View {
	var v tea.View
	v.AltScreen = true
	// Мышь нужна ради кликов по статусной строке и окнам. В режиме терминала
	// она отдаётся оболочке внутри окна: там мышь может просить сама
	// оболочка, а не dmsh.
	if m.mouseEnabled() {
		if m.state == tuiTerminal {
			// Движение без нажатия нужно только когда программа внутри
			// окна включила режим 1003: иначе терминал шлёт поток событий
			// впустую.
			if mode := m.terminalMouseMode(); mode.Track != vt.MouseOff {
				v.MouseMode = tea.MouseModeCellMotion
				if mode.WantsMotion() {
					v.MouseMode = tea.MouseModeAllMotion
				}
			}
		} else {
			v.MouseMode = tea.MouseModeCellMotion
		}
	}
	v.SetContent(m.render())

	// Терминал рисует курсор сам: он принадлежит оболочке, а не строке
	// ввода dmsh. Поверх открытого окна курсор остаётся в окне — иначе
	// правка в палитре шла бы не туда.
	box, hasModal := m.modal()
	if !hasModal {
		if pos, ok := m.terminalCursor(); ok {
			v.Cursor = &tea.Cursor{Position: pos, Shape: tea.CursorBlock}
			return v
		}
	}

	if hasModal && m.height > 0 && m.width > 0 {
		_, col, y := overlayModal(m.renderFrame(), box, m.width, m.height)
		if m.height > 1 && y > m.height-2 {
			y = m.height - 2
		}
		v.Cursor = &tea.Cursor{Position: tea.Position{X: col, Y: y}, Shape: tea.CursorBlock, Blink: true}
		return v
	}

	var col int
	switch m.state {
	case tuiSearch:
		col = displayWidth(m.searchPrompt()) + runeSliceLen(m.input)
	case tuiModelMenu:
		col = 0
	default:
		col = displayWidth(m.buildPrompt())
		for _, r := range []rune(m.input)[:min(m.cursorPos, runeSliceLen(m.input))] {
			col += displayWidth(string(r))
		}
	}
	y := m.inputRow()
	switch m.state {
	case tuiModelMenu:
		// Строки меню занимают место строки ввода, поэтому курсор встаёт на
		// выделенный пункт. Позиция считается той же раскладкой, что и клик:
		// со строкой фильтра и со скроллом список смещается, и «+1» тут
		// было бы ошибкой.
		_, first := m.menuRows()
		start, _ := m.menuWindow()
		y = m.inputRow() + first + m.modelIdx - start
		if m.height > 1 && y > m.height-2 {
			y = m.height - 2
		}
	case tuiSearch:
		// Строка поиска живёт в скроллбеке: вернуться к блоку, который её
		// содержит.
		y -= len(hardWrap(m.searchPrompt()+colorGreen+m.input, m.width))
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

// wrappedContent возвращает content, свёрнутый по текущей ширине, и
// переиспользует строки прошлого кадра. Смена ширины, обрезка скроллбека
// или другое содержимое сбрасывают кеш: тогда всё пересчитывается.
func (m tuiModel) wrappedContent() []string {
	if m.content == "" {
		return nil
	}
	complete, tail := m.content, ""
	if i := strings.LastIndexByte(m.content, '\n'); i >= 0 {
		complete, tail = m.content[:i+1], m.content[i+1:]
	} else {
		complete, tail = "", m.content
	}

	c := m.bodyCache
	var rows []string
	if c != nil && c.width == m.width && c.highlight == m.findQuery && strings.HasPrefix(complete, c.base) {
		// Хвост прошлого кадра отбрасывается: он входит в complete заново.
		rows = c.rows[:c.completeRows]
		rows = append(rows, wrapText(complete[len(c.base):], m.width, m.findQuery)...)
	} else {
		rows = wrapText(complete, m.width, m.findQuery)
	}
	cacheRows := len(rows)
	rows = append(rows, wrapText(tail, m.width, m.findQuery)...)
	if c != nil {
		c.width = m.width
		c.base = complete
		c.rows = rows
		c.completeRows = cacheRows
		c.highlight = m.findQuery
	}
	return rows
}

// wrapText режет текст на строки и сворачивает каждую по ширине. Один
// завершающий перевод строки не создаёт пустой строки — он просто закрывает
// последнюю. Совпадения поиска подсвечиваются после переноса: маркеры не
// должны попасть под разрез строки.
func wrapText(text string, width int, highlight string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	rows := make([]string, 0, len(lines))
	for _, l := range lines {
		for _, row := range hardWrap(l, width) {
			if highlight != "" {
				row = highlightMatches(row, highlight)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// layoutRows builds the scrollable body rows and the input display rows for
// the current width, wrapping any row wider than the terminal. Without this
// the terminal soft-wraps long lines, so the frame no longer matches the
// screen rows: the status line breaks apart and the render falls into garbage.
func (m tuiModel) layoutRows() (bodyRows, inRows []string) {
	// Терминал занимает весь кадр: ни транскрипта, ни строки ввода.
	if m.state == tuiTerminal {
		return m.terminalRows(), nil
	}
	bodyRows = m.wrappedContent()
	if m.state == tuiSearch {
		bodyRows = append(bodyRows, hardWrap(m.searchPrompt()+colorGreen+m.input, m.width)...)
	}

	switch m.state {
	case tuiModelMenu:
		inRows, _ = m.menuRows()
	case tuiConfirming, tuiSearch:
		// Обе строки живут в скроллбеке, отдельной строки ввода нет: у
		// подтверждения вместо неё окно с кнопками.
	default:
		inRows = hardWrap(m.buildPrompt()+m.input, m.width)
		if m.showActionBar() {
			inRows = append(inRows, fitWidth(m.actionBarRow(), m.width))
		}
		if m.streaming {
			inRows = hardWrap(fmt.Sprintf("%s[dmsh] thinking…%s", colorCyan, colorReset), m.width)
		} else if m.running {
			// Команда выполняется в фоне: ввод свободен, видно, что она идёт
			// и сколько уже идёт, а её вывод печатается выше по мере
			// поступления.
			inRows = append(inRows, hardWrap(m.runningLine(), m.width)...)
		}
		// Slash-command menu with descriptions while typing "/". В окне поиска
		// по выводу оно не нужно: запрос может начинаться со слэша.
		if m.state != tuiFind {
			const maxVisible = 8
			menu := m.menuList()
			start, end := scrollWindow(m.tabIdx, len(menu), maxVisible)
			for i, cmd := range menu[start:end] {
				marker, style := "  ", colorGray
				descStyle := colorGray
				if start+i == m.tabIdx {
					marker, style = "> ", colorYellow+colorBold
					descStyle = ""
				}
				item := fmt.Sprintf("%s%s%s%s %s— %s%s", style, marker, cmd, colorReset, descStyle, slashDesc[cmd], colorReset)
				inRows = append(inRows, hardWrap(fitWidth(item, m.width), m.width)...)
			}
			inRows = append(inRows, m.listFooter(start, end, len(menu), maxVisible)...)
		}
	}
	return bodyRows, inRows
}

// scrollWindow возвращает полуинтервал видимых строк списка так, чтобы
// выделение всегда было на экране. Без этого длинный список упирается в
// «(+N more)»: стрелки двигают выделение, а показать его нечем.
func scrollWindow(selected, total, size int) (start, end int) {
	if total <= size {
		return 0, total
	}
	start = selected - size/2
	if start < 0 {
		start = 0
	}
	if start > total-size {
		start = total - size
	}
	return start, start + size
}

// listFooter подсказывает, что список длиннее окна и какая его часть
// показана. Пустая строка означает, что всё уместилось.
func (m tuiModel) listFooter(start, end, total, size int) []string {
	if total <= size {
		return nil
	}
	return []string{colorGray + fmt.Sprintf("(%d–%d of %d, ↑/↓)", start+1, end, total) + colorReset}
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
	frame := m.renderFrame()
	// Модальные окна (палитра, настройки) рисуются поверх готового кадра:
	// так содержимое под ними остаётся видимым и не приходится держать
	// отдельную раскладку строк ввода для каждого состояния.
	if box, ok := m.modal(); ok && m.height > 0 && m.width > 0 {
		frame, _, _ = overlayModal(frame, box, m.width, m.height)
	}
	return strings.Join(frame, "\n")
}

// modal возвращает содержимое открытого модального окна. Пустой заголовок
// означает «окна нет».
func (m tuiModel) modal() (modalBox, bool) {
	box := m.modalBox()
	return box, box.title != ""
}

// modalBox — то же окно, но без признака «открыто»: пустой заголовок значит,
// что окна нет.
func (m tuiModel) modalBox() modalBox {
	switch m.state {
	case tuiPalette:
		return m.paletteModal()
	case tuiSettings:
		return m.settingsModal()
	case tuiSetup:
		return m.setupModal()
	case tuiModeMenu:
		return m.modeMenuModal()
	case tuiDirMenu:
		return m.dirMenuModal()
	case tuiConfirming:
		return m.confirmModal()
	case tuiTodoMenu:
		return m.todoModal()
	}
	return modalBox{}
}

// renderFrame рисует всё, кроме модального окна: скроллбек, строку ввода и
// статусную строку.
func (m tuiModel) renderFrame() []string {
	bodyLines, inLines := m.layoutRows()
	status := fitWidth(m.statusline(), m.width)

	if m.height <= 0 {
		return append(bodyLines, inLines...)
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
	// Полоса живёт поверх транскрипта: она нужна ровно тогда, когда вывод не
	// помещается, и её ширина в одну колонку платится только в этом случае.
	return m.withScrollbar(frame, len(visible))
}

// statusDirLabel — текущая папка в квадратных скобках в начале статусной
// строки. Это же и текст, по которому кликают мышью: отдельная копия пути
// в обработчике клика рано или поздно разошлась бы с нарисованной.
func statusDirLabel() string {
	wd, _ := os.Getwd()
	return "[" + shortPath(wd) + "]"
}

// statusSegments собирает статусную строку из кусочков, у каждого которых
// есть действие клика. Отрисовка и попадание клика идут по одному списку:
// иначе кнопка и её границы рано или поздно разъедутся.
func (m tuiModel) statusSegments() (left, right []statusSegment) {
	left = []statusSegment{
		{text: colorCyan + statusDirLabel() + colorReset, act: clickOpenDir},
		{text: " "},
		{text: colorYellow + "mode:" + m.modeLabel + colorReset, act: clickOpenModeMenu},
		{text: "  "},
		{text: colorCyan + "model:" + m.modelLabel() + colorReset, act: clickOpenModelMenu},
		{text: "  "},
		{text: gray + m.proxyLabel() + colorReset, act: clickOpenSettings},
	}
	right = []statusSegment{
		{text: colorGray + "help" + colorReset, act: clickShowHelp},
		{text: " "},
		{text: gray + "F1"},
		{text: "   "},
		{text: colorGray + "palette" + colorReset, act: clickOpenPalette},
		{text: " "},
		{text: gray + "Ctrl+P"},
		{text: "   "},
		{text: colorGray + "mode" + colorReset, act: clickCycleMode},
		{text: " "},
		{text: gray + "Shift+Tab"},
	}
	return left, right
}

// statusLine собирает строку целиком: слева состояние, справа подсказки, между
// ними — пустое место до края терминала.
func (m tuiModel) statusLine() string {
	left, right := m.statusSegments()
	ls, rs := segmentsWidth(left), segmentsWidth(right)
	pad := m.width - ls - rs
	sep := " "
	if pad >= 1 {
		sep = strings.Repeat(" ", pad)
	}
	return segmentsText(left) + sep + segmentsText(right)
}

func (m tuiModel) statusline() string {
	return fitWidth(m.statusLine(), m.width)
}

// proxyLabel показывает, пойдёт ли ближайший сетевой запрос через прокси.
// Молчаливое «прокси есть» обнаруживается уже по отказу соединения, а по
// строке состояния — сразу.
func (m tuiModel) proxyLabel() string {
	if m.sessionProxy().Enabled() {
		return "proxy:on"
	}
	return "proxy:off"
}

// modelLabel показывает удалённую модель с именем провайдера, а для
// локального GGUF — имя файла. Пустая строка на старте — это "none".
func (m tuiModel) modelLabel() string {
	provider := currentProvider(m.s, m.rf.cfg.Provider)
	if showsRemoteProvider(provider, m.localModelPath()) {
		model := ""
		if m.s != nil {
			model = m.s.cfg.RemoteModel
		}
		return string(provider) + ":" + firstNonEmpty(model, config.DefaultModelFor(provider))
	}
	return shortModelName(m.localModelPath())
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
	if m.state == tuiFind {
		return m.findPrompt()
	}
	cwd, _ := os.Getwd()
	short := shortPath(cwd)
	return fmt.Sprintf("%s[%s]%s > %s", colorGray, short, colorReset, colorReset)
}

// runningLine — строка «идёт команда». Одна на экран: и раскладка, и поиск
// кликабельной подсказки «↓N» считают её отсюда.
func (m tuiModel) runningLine() string {
	line := fmt.Sprintf("%s… running %s (%s", colorYellow, shortCmd(m.cmdCommand), m.cmdElapsed())
	if m.scrollOffset > 0 {
		// Пользователь ушёл читать выше, а лог растёт: сколько строк внизу —
		// это ровно scrollOffset, и клик по подсказке возвращает в конец.
		line += fmt.Sprintf(", %s↓%d below%s", colorCyan, m.scrollOffset, colorReset)
	}
	return line + ", Esc cancels)" + colorReset
}

// addLine печатает строку в транскрипт.
func (m *tuiModel) addLine(text string) {
	m.content = appendLine(m.content, text)
	m.countScrollback(1)
}

// clearContent очищает транскрипт: счётчик строк тоже сбрасывается, иначе
// следующая же обрезка снесла бы половину свежего экрана.
func (m *tuiModel) clearContent() {
	m.content = ""
	m.scrollOffset = 0
	m.scrollbackLines = 0
	m.clearFind()
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

func displayWidth(s string) int {
	return ansi.StringWidth(s)
}

// rowOfLine — экранная строка, с которой начинается логическая строка line.
func (m tuiModel) rowOfLine(line int) int {
	lines := strings.Split(m.content, "\n")
	row := 0
	for i := 0; i < line && i < len(lines); i++ {
		row += len(hardWrap(lines[i], m.width))
	}
	return row
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

