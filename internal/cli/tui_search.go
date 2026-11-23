package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

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
