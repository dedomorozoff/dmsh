package vt

import (
	"strings"
	"testing"
)

// Строки экрана без escape-последовательностей: так проверяем именно
// содержимое, а не оформление.
func lines(s *Screen) []string { return s.Text() }

func TestPrintsPlainText(t *testing.T) {
	s := NewScreen(20, 3)
	s.WriteString("hello")
	if got := lines(s)[0]; got != "hello" {
		t.Fatalf("row 0 = %q, want %q", got, "hello")
	}
}

func TestNewlineMovesDownAndCarriageReturnRewinds(t *testing.T) {
	s := NewScreen(20, 3)
	s.WriteString("first\r\nsecond")
	got := lines(s)
	if got[0] != "first" || got[1] != "second" {
		t.Fatalf("screen = %q, want first/second", got)
	}
	// \r без \n просто двигает курсор в начало строки.
	s.WriteString("\rXY")
	if got := lines(s)[1]; got != "XYcond" {
		t.Fatalf("row 1 = %q, want %q", got, "XYcond")
	}
}

func TestScrollingDropsOldestRow(t *testing.T) {
	s := NewScreen(10, 2)
	s.WriteString("one\r\ntwo\r\nthree")
	got := lines(s)
	if got[0] != "two" || got[1] != "three" {
		t.Fatalf("screen = %q, want two/three", got)
	}
}

// Приглашение оболочки перерисовывается на той же строке: без поддержки
// \r и стирания строки в выводе остаются хвосты предыдущих строк.
func TestPromptRedrawOnSameLine(t *testing.T) {
	s := NewScreen(30, 2)
	s.WriteString("\r\x1b[Knew prompt>")
	if got := lines(s)[0]; got != "new prompt>" {
		t.Fatalf("row 0 = %q, want %q", got, "new prompt>")
	}
}

func TestEraseDisplayClearsScreen(t *testing.T) {
	s := NewScreen(20, 3)
	s.WriteString("one\r\ntwo\r\nthree")
	s.WriteString("\x1b[2J")
	for i, row := range lines(s) {
		if row != "" {
			t.Fatalf("row %d = %q, want empty after \\x1b[2J", i, row)
		}
	}
}

func TestCursorMovement(t *testing.T) {
	s := NewScreen(20, 5)
	s.WriteString("\x1b[2;3Hxy")
	cx, cy := s.Cursor()
	if cx != 4 || cy != 1 {
		t.Fatalf("cursor = (%d,%d), want (4,1)", cx, cy)
	}
	s.WriteString("\x1b[A")
	if _, cy := s.Cursor(); cy != 0 {
		t.Fatalf("after CUU cy = %d, want 0", cy)
	}
	s.WriteString("\x1b[2B\x1b[1D")
	cx, cy = s.Cursor()
	if cx != 3 || cy != 2 {
		t.Fatalf("cursor = (%d,%d), want (3,2)", cx, cy)
	}
}

// Backspace затирает символ: оболочки правят строку ввода им.
func TestBackspaceErases(t *testing.T) {
	s := NewScreen(20, 2)
	s.WriteString("abcXX\b\b  ")
	if got := lines(s)[0]; got != "abc" {
		t.Fatalf("row 0 = %q, want %q", got, "abc")
	}
}

func TestSGRIsKeptInLines(t *testing.T) {
	s := NewScreen(10, 1)
	s.WriteString("\x1b[31mred\x1b[0m")
	row := s.Lines()[0]
	if !strings.Contains(row, "\x1b[31m") {
		t.Fatalf("color escape lost: %q", row)
	}
	if !strings.Contains(row, "red") {
		t.Fatalf("text lost: %q", row)
	}
}

func TestTabMovesToStop(t *testing.T) {
	s := NewScreen(20, 1)
	s.WriteString("a\tb")
	cx, _ := s.Cursor()
	if cx != 9 {
		t.Fatalf("cursor = %d, want 9 (next stop after one tab)", cx)
	}
}

// Поток приходит кусками: символ и escape-последовательность могут быть
// разрезаны пополам.
func TestSplitWrites(t *testing.T) {
	s := NewScreen(10, 2)
	full := "\x1b[31mпривет"
	for i := 0; i < len(full); i++ {
		s.Write([]byte{full[i]})
	}
	if got := lines(s)[0]; got != "привет" {
		t.Fatalf("row 0 = %q, want %q", got, "привет")
	}
	if !strings.Contains(s.Lines()[0], "\x1b[31m") {
		t.Fatalf("color lost across a split write: %q", s.Lines()[0])
	}
}

// Неизвестные последовательности не должны оседать на экране текстом.
func TestUnknownSequencesAreSwallowed(t *testing.T) {
	s := NewScreen(20, 2)
	s.WriteString("\x1b]0;window title\x07\x1b[?25l\x1b(Bvisible")
	if got := lines(s)[0]; got != "visible" {
		t.Fatalf("row 0 = %q, want %q", got, "visible")
	}
}

func TestResizeKeepsContent(t *testing.T) {
	s := NewScreen(20, 4)
	s.WriteString("hello")
	s.Resize(10, 2)
	cols, rows := s.Size()
	if cols != 10 || rows != 2 {
		t.Fatalf("size = %dx%d, want 10x2", cols, rows)
	}
	if got := lines(s)[0]; got != "hello" {
		t.Fatalf("row 0 = %q, want %q", got, "hello")
	}
}

func TestWrapToNextLine(t *testing.T) {
	s := NewScreen(4, 3)
	s.WriteString("abcdefg")
	got := lines(s)
	if got[0] != "abcd" || got[1] != "efg" {
		t.Fatalf("screen = %q, want abcd/efg", got)
	}
}
