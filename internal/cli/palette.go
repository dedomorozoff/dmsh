package cli

import (
	"fmt"
	"strings"

	"charm.land/bubbletea/v2"
)

// paletteItem — одна строка панели команд (Ctrl+P). Выполняется через
// handleSlash, поэтому панель не может разойтись со слэш-командами.
type paletteItem struct {
	name string
	desc string
	// label — что показать вместо имени. Для режимов нужноhuman-readable
	// имя, а не "/mode shell".
	label string
	// key запускает команду напрямую. Пусто = выполняется name.
	key string
}

// paletteItems — команды панели. Порядок фиксирован: он и есть порядок
// показа при пустом запросе.
var paletteItems = []paletteItem{
	{name: "/shell", label: "terminal: open a shell", desc: "hand the terminal over to an interactive shell"},
	{name: "/help", desc: "show help for commands and modes"},
	{name: "/bind", desc: "show key bindings"},
	{name: "/exit", desc: "exit dmsh"},
	{name: "/clear", desc: "clear screen"},
	{name: "/model", desc: "show the current model"},
	{name: "/todo", desc: "show the model's task list"},
	{name: "/stats", desc: "session statistics"},
	{name: "/history", desc: "recent commands"},
	{name: "/retry", desc: "retry the last request"},
	{name: "/alias", desc: "list or create aliases"},
	{name: "/export", desc: "export the last command"},
	{name: "/cd", desc: "change directory"},
	{name: "/pwd", desc: "current directory"},
	{name: "mode:ai", label: "mode: AI (auto-execute)", desc: "run the model's commands automatically", key: "/mode ai"},
	{name: "mode:help", label: "mode: Help (explain only)", desc: "show commands with explanations, never execute", key: "/mode help"},
	{name: "mode:shell", label: "mode: Terminal (direct shell)", desc: "open a real terminal session", key: "/mode shell"},
}

// paletteDisplay возвращает текст, который показывается в строке палитры.
func (it paletteItem) paletteDisplay() string {
	if it.label != "" {
		return it.label
	}
	return it.name
}

// paletteCommand возвращает строку, которую нужно выполнить.
func (it paletteItem) paletteCommand() string {
	if it.key != "" {
		return it.key
	}
	return it.name
}

// paletteMatches фильтрует палитру по запросу: сначала префиксы имени,
// затем вхождения в имя или описание.
func paletteMatches(query string) []paletteItem {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]paletteItem(nil), paletteItems...)
	}
	var prefix, other []paletteItem
	for _, it := range paletteItems {
		// Ищем и по показываемой подписи, и по внутреннему имени: в режиме
		// терминала подпись многословная, а набирать удобно короткое
		// "mode:shell".
		names := []string{strings.ToLower(it.paletteDisplay()), strings.ToLower(it.name)}
		desc := strings.ToLower(it.desc)
		switch {
		case hasPrefixAny(names, q):
			prefix = append(prefix, it)
		case containsAny(names, q) || strings.Contains(desc, q):
			other = append(other, it)
		}
	}
	return append(prefix, other...)
}

func hasPrefixAny(list []string, q string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, q) {
			return true
		}
	}
	return false
}

func containsAny(list []string, q string) bool {
	for _, s := range list {
		if strings.Contains(s, q) {
			return true
		}
	}
	return false
}

// paletteHeader подсказывает управление и печатается над списком.
const paletteHeader = "command palette — type to filter, ↑/↓ pick, Enter run, Esc close"

// openPalette запоминает текущую строку ввода и открывает панель.
func (m tuiModel) openPalette() tuiModel {
	m.state = tuiPalette
	m.paletteSaved = m.input
	m.paletteIdx = 0
	m.input = ""
	m.cursorPos = 0
	m.tabMatches = nil
	m.tabIdx = -1
	return m
}

// closePalette возвращает строку ввода, которую сохранил openPalette.
func (m tuiModel) closePalette() tuiModel {
	m.state = tuiIdle
	m.input = m.paletteSaved
	m.cursorPos = runeSliceLen(m.input)
	m.paletteSaved = ""
	m.paletteIdx = 0
	m.tabMatches = nil
	m.tabIdx = -1
	return m
}

// paletteMove сдвигает выделение на dir (-1 вверх, +1 вниз) и держит его
// в границах текущего списка.
func (m *tuiModel) paletteMove(dir int) {
	n := len(paletteMatches(m.input))
	if n == 0 {
		m.paletteIdx = 0
		return
	}
	if m.paletteIdx < 0 {
		m.paletteIdx = 0
	}
	m.paletteIdx += dir
	if m.paletteIdx < 0 {
		m.paletteIdx = 0
	}
	if m.paletteIdx >= n {
		m.paletteIdx = n - 1
	}
}

// handlePaletteKey обрабатывает клавиши в режиме палитры. Запрос живёт в
// обычной строке ввода, поэтому нажатия на текст обрабатываются так же,
// как в обычном режиме.
func (m tuiModel) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape || msg.Keystroke() == "ctrl+c":
		return m.closePalette(), nil
	case msg.Code == tea.KeyEnter:
		return m.runPaletteItem()
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.paletteMove(-1)
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.paletteMove(1)
		return m, nil
	case msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h":
		m.deleteBackward()
		m.paletteIdx = 0
		return m, nil
	}
	if isInputKey(msg) {
		m.input = insertAt(m.input, m.cursorPos, msg.Text)
		m.cursorPos = runeSliceLen(m.input)
		m.paletteIdx = 0
	}
	return m, nil
}

// runPaletteItem выполняет выделенный пункт. Пустой список — запрос не
// совпал ни с чем, палитра остаётся открытой.
func (m tuiModel) runPaletteItem() (tea.Model, tea.Cmd) {
	items := paletteMatches(m.input)
	if len(items) == 0 {
		return m, nil
	}
	if m.paletteIdx >= len(items) {
		m.paletteIdx = len(items) - 1
	}
	cmd := items[m.paletteIdx].paletteCommand()
	return m.closePalette().handleSlash(cmd)
}

// paletteRows рисует заголовок и отфильтрованный список. Используется вместо
// строки ввода, пока палитра открыта.
func (m tuiModel) paletteRows() []string {
	rows := hardWrap(fitWidth(fmt.Sprintf("%s%s%s", colorBold+colorCyan, m.paletteQueryRow(), colorReset), m.width), m.width)
	items := paletteMatches(m.input)
	if len(items) == 0 {
		return append(rows, hardWrap(fitWidth(fmt.Sprintf("%sno matching command%s", colorGray, colorReset), m.width), m.width)...)
	}
	// Длинные списки не должны вытеснить строку ввода за экран.
	const maxVisible = 10
	for i, it := range items {
		if i >= maxVisible {
			if rest := len(items) - maxVisible; rest > 0 {
				rows = append(rows, hardWrap(fitWidth(colorGray+fmt.Sprintf("(+%d more)", rest)+colorReset, m.width), m.width)...)
			}
			break
		}
		marker, style := "  ", colorGray
		descStyle := colorGray
		if i == m.paletteIdx {
			marker, style = "> ", colorYellow+colorBold
			descStyle = ""
		}
		row := fmt.Sprintf("%s%s%s%s %s— %s%s", style, marker, it.paletteDisplay(), colorReset, descStyle, it.desc, colorReset)
		rows = append(rows, hardWrap(fitWidth(row, m.width), m.width)...)
	}
	return rows
}

// paletteQueryRow — строка запроса: приглашение плюс введённый текст.
func (m tuiModel) paletteQueryRow() string {
	if m.input == "" {
		return "palette> " + paletteHeader
	}
	return fmt.Sprintf("palette> %s%s", m.input, paletteHeader)
}
