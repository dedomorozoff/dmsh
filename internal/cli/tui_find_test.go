package cli

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
)

func TestContainsFold(t *testing.T) {
	cases := []struct {
		text, query string
		want        bool
	}{
		{"Hello World", "hello", true},
		{"Привет, мир", "ПРИВЕТ", true},
		{"abcdef", "cde", true},
		{"abc", "abcd", false},
		{"", "x", false},
		{"anything", "", true},
	}
	for _, c := range cases {
		if got := containsFold(c.text, c.query); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", c.text, c.query, got, c.want)
		}
	}
}

func TestHighlightMatchesKeepsANSI(t *testing.T) {
	line := colorGreen + "alpha beta alpha" + colorReset
	got := highlightMatches(line, "alpha")
	if strings.Count(got, "\x1b[7m") != 2 {
		t.Fatalf("expected 2 highlighted spans: %q", got)
	}
	if !strings.Contains(got, "\x1b[7malpha\x1b[27m") {
		t.Fatalf("highlight markers do not wrap the match: %q", got)
	}
	if !strings.Contains(got, "beta") || !strings.Contains(got, colorGreen) {
		t.Fatalf("the rest of the line was lost: %q", got)
	}
}

func TestHighlightMatchesIgnoresEscapeDigits(t *testing.T) {
	// 38 и 5 — байты цвета, а не текст. Иначе подсветка сломала бы строку.
	line := "\x1b[38;5;123mtext\x1b[0m"
	got := highlightMatches(line, "38")
	if got != line {
		t.Fatalf("matched inside an escape sequence: %q", got)
	}
}

func TestHighlightMatchesCaseInsensitive(t *testing.T) {
	got := highlightMatches("ПриВет", "привет")
	if !strings.Contains(got, "\x1b[7mПриВет\x1b[27m") {
		t.Fatalf("case-insensitive match not highlighted: %q", got)
	}
}

func TestSearchSlashOpensFindAndHighlights(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 12
	m.appendScrollback("alpha beta\nsecond ALPHA line\nnothing here\n")

	m.input = "/search alpha"
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)
	if m.state != tuiFind {
		t.Fatalf("state = %d, want tuiFind", m.state)
	}
	if len(m.findMatches) != 2 {
		t.Fatalf("matches = %v, want 2 lines", m.findMatches)
	}
	if m.findIdx != 1 {
		t.Fatalf("findIdx = %d, want the newest match", m.findIdx)
	}
	if m.input != "alpha" {
		t.Fatalf("query in the input line = %q", m.input)
	}
	rows := m.wrappedContent()
	joined := strings.Join(rows, "\n")
	if strings.Count(joined, "\x1b[7m") != 2 {
		t.Fatalf("expected both matches highlighted: %q", joined)
	}
	if !strings.Contains(m.render(), "\x1b[7m") {
		t.Fatal("render should keep the highlight")
	}
}

func TestFindStepsThroughMatches(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	m.appendScrollback("one\nneedle\ntwo\nneedle\nthree\n")

	m = press(m, keyText("/"))
	m.input = "/search needle"
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)

	first := m.findIdx
	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyF3})
	m = mm.(tuiModel)
	if m.findIdx == first {
		t.Fatalf("F3 did not move from %d", first)
	}
	second := m.findIdx
	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyF3, Mod: tea.ModShift})
	m = mm.(tuiModel)
	if m.findIdx != first {
		t.Fatalf("Shift+F3 went to %d, want %d", m.findIdx, first)
	}
	if second != 0 {
		t.Fatalf("F3 wrapped to %d, want 0", second)
	}
}

func TestFindEnterKeepsEscClearsHighlight(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	m.appendScrollback("keep me\n")

	m.input = "/find keep"
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)

	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	if m.state != tuiIdle {
		t.Fatalf("Enter should close the find window, state = %d", m.state)
	}
	if m.findQuery != "keep" {
		t.Fatalf("Enter dropped the query: %q", m.findQuery)
	}
	if !strings.Contains(strings.Join(m.wrappedContent(), "\n"), "\x1b[7m") {
		t.Fatal("Enter should keep the highlight")
	}

	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mm.(tuiModel)
	if m.findQuery != "" || len(m.findMatches) != 0 {
		t.Fatalf("Esc did not clear the search: %q %v", m.findQuery, m.findMatches)
	}
	if strings.Contains(strings.Join(m.wrappedContent(), "\n"), "\x1b[7m") {
		t.Fatal("Esc should remove the highlight")
	}
}

func TestFindScrollsMatchIntoView(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	var b strings.Builder
	for i := 0; i < 200; i++ {
		if i == 42 {
			b.WriteString("the NEEDLE is here\n")
		} else {
			b.WriteString("ordinary output line\n")
		}
	}
	m.appendScrollback(b.String())

	m.input = "/search needle"
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)

	if !strings.Contains(m.render(), "NEEDLE") {
		t.Fatal("jump should scroll the match into the visible frame")
	}
}

func TestCtrlFOpensFindWithLastQuery(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	m.appendScrollback("repeat me\n")
	m.findQuery = "repeat"

	mm, _ := m.handleKey(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = mm.(tuiModel)
	if m.state != tuiFind {
		t.Fatalf("Ctrl+F should open find, state = %d", m.state)
	}
	if m.input != "repeat" {
		t.Fatalf("Ctrl+F should reuse the last query, got %q", m.input)
	}
}

func TestCopyLastBlock(t *testing.T) {
	got := withClipboardStub(t)
	m := newTestTui()
	m.appendScrollback("first line\nsecond line\n\nblock a\nblock b\n")

	m.handleCopy("/copy")
	if *got != "block a\nblock b" {
		t.Fatalf("copied %q, want the last block", *got)
	}
}

func TestCopyLastLines(t *testing.T) {
	got := withClipboardStub(t)
	m := newTestTui()
	m.appendScrollback("one\ntwo\nthree\nfour\n")

	m.handleCopy("/copy 2")
	if *got != "three\nfour" {
		t.Fatalf("copied %q, want the last two lines", *got)
	}
}

func TestCopyStripsANSI(t *testing.T) {
	got := withClipboardStub(t)
	m := newTestTui()
	m.addLine(colorGreen + "colored output" + colorReset)

	m.handleCopy("/copy")
	if *got != "colored output" {
		t.Fatalf("copied %q, want plain text", *got)
	}
}

func TestCopyUnknownArgShowsUsage(t *testing.T) {
	withClipboardStub(t)
	m := newTestTui()
	m.appendScrollback("text\n")
	before := m.content
	m.handleCopy("/copy nope")
	if m.content == before {
		t.Fatal("expected a usage message")
	}
	if !strings.Contains(m.content, "usage: /copy") {
		t.Fatalf("usage hint missing: %q", m.content)
	}
}

func TestCopyEmptyTranscript(t *testing.T) {
	withClipboardStub(t)
	m := newTestTui()
	m.handleCopy("/copy")
	if !strings.Contains(m.content, "nothing to copy") {
		t.Fatalf("expected an empty-transcript message, got %q", m.content)
	}
}

func TestCopyClipboardError(t *testing.T) {
	old := clipboardWrite
	clipboardWrite = func(string) error { return errors.New("no clipboard") }
	t.Cleanup(func() { clipboardWrite = old })

	m := newTestTui()
	m.appendScrollback("text\n")
	m.handleCopy("/copy")
	if !strings.Contains(m.content, "Clipboard unavailable") {
		t.Fatalf("expected a clipboard error, got %q", m.content)
	}
}

// withClipboardStub подменяет буфер обмена и возвращает указатель на
// последний скопированный текст.
func withClipboardStub(t *testing.T) *string {
	t.Helper()
	var got string
	old := clipboardWrite
	clipboardWrite = func(s string) error {
		got = s
		return nil
	}
	t.Cleanup(func() { clipboardWrite = old })
	return &got
}
