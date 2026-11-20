package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// handleCopy копирует вывод без мыши. Без аргумента — последний непустой
// блок (ответ модели или хвост лога), с числом — последние N строк. Мышь в
// TUI занята кликами; /copy даёт тот же результат с клавиатуры.
func (m *tuiModel) handleCopy(line string) {
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/copy"))
	text, lines, err := m.copyPayload(arg)
	if err != nil {
		m.addLine(fmt.Sprintf("%s%s%s", colorYellow, err, colorReset))
		return
	}
	if err := clipboardWrite(text); err != nil {
		m.addLine(fmt.Sprintf("%sClipboard unavailable: %v%s", colorYellow, err, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("%s✓ Copied %d line(s) to clipboard%s", colorGreen, lines, colorReset))
}

// copyPayload собирает текст для буфера обмена из видимого транскрипта.
func (m tuiModel) copyPayload(arg string) (string, int, error) {
	lines := strings.Split(ansi.Strip(m.content), "\n")
	// Транскрипт почти всегда кончается переводом строки: хвостовые пустые
	// строки не входят в блок.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", 0, fmt.Errorf("nothing to copy yet")
	}
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			return "", 0, fmt.Errorf("usage: /copy [N] — last block, or the last N lines")
		}
		if n > len(lines) {
			n = len(lines)
		}
		lines = lines[len(lines)-n:]
		return strings.Join(lines, "\n"), len(lines), nil
	}
	// Последний блок — непрерывные непустые строки от конца.
	start := len(lines) - 1
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	lines = lines[start:]
	return strings.Join(lines, "\n"), len(lines), nil
}
