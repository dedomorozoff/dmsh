package cli

import (
	tea "charm.land/bubbletea/v2"
)

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
	old := *pos
	*pos = i
	out := make([]rune, 0, len(runes)-(old-i))
	out = append(out, runes[:i]...)
	out = append(out, runes[old:]...)
	return string(out)
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
