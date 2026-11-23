package cli

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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
