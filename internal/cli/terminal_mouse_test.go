package cli

import (
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/vt"
)

// mouseTerminalTui — окно терминала, в котором программа включила мышь: dmsh
// должен и запросить события у своего терминала, и переслать их дальше.
func mouseTerminalTui(t *testing.T) tuiModel {
	t.Helper()
	m := newTerminalTui()
	m.width, m.height = 40, 10
	m.term.screen.Resize(40, 9)
	m.term.screen.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	return m
}

// Пока программа внутри окна не включила мышь, события остаются у dmsh:
// иначе клики по его интерфейсу перехватывались бы молча.
func TestTerminalKeepsMouseUntilAppAsks(t *testing.T) {
	m := newTerminalTui()
	m.width, m.height = 40, 10
	m.term.screen.Resize(40, 9)
	if mode := m.terminalMouseMode(); mode.Track != vt.MouseOff {
		t.Fatalf("a plain shell must not get the mouse, got %+v", mode)
	}
	if got := m.renderView().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("view must not request mouse events for a plain shell, got %v", got)
	}
	// Клик по строке состояния в терминале не делает ничего.
	if got := click(m, 0, m.height-1); got.state != tuiTerminal {
		t.Fatalf("a click in terminal mode changed the state to %v", got.state)
	}
}

// Программа попросила мышь — dmsh включает свою и пересылает события в
// псевдотерминал. Раньше включение молча глоталось, и vim внутри окна не
// получал ничего.
func TestTerminalAsksMouseWhenAppEnablesIt(t *testing.T) {
	m := mouseTerminalTui(t)
	mode := m.terminalMouseMode()
	if mode.Track != vt.MouseClick || !mode.SGR {
		t.Fatalf("the app request must be seen: %+v", mode)
	}
	if got := m.renderView().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion", got)
	}
}

// Движение без нажатия нужно только для режима 1003: иначе терминал шлёт в
// псевдотерминал поток событий впустую.
func TestTerminalRequestsMotionOnlyForAnyTracking(t *testing.T) {
	m := mouseTerminalTui(t)
	if got := m.renderView().MouseMode; got == tea.MouseModeAllMotion {
		t.Fatal("1000 must not enable motion reporting")
	}
	m.term.screen.Write([]byte("\x1b[?1003h"))
	if got := m.renderView().MouseMode; got != tea.MouseModeAllMotion {
		t.Fatalf("mouse mode = %v, want all motion after 1003", got)
	}
}

// Координаты клика переводятся в систему терминала: строки экрана смещены на
// строку состояния, а нумерация — с единицы.
func TestTerminalMouseCoordinates(t *testing.T) {
	m := mouseTerminalTui(t)
	ox, oy := m.terminalOrigin()
	if ox != 0 || oy != 0 {
		t.Fatalf("the terminal screen starts at (%d,%d), want the top-left corner", ox, oy)
	}
	ev := m.mouseEvent(vt.MousePress, vt.MouseLeft, tea.ModCtrl, 3, 4)
	if ev.Col != 4 || ev.Row != 5 {
		t.Fatalf("event coords = (%d,%d), want (4,5): one-based screen position", ev.Col, ev.Row)
	}
	if !ev.Ctrl {
		t.Fatal("modifiers must reach the terminal")
	}
	if got := string(vt.EncodeMouse(m.terminalMouseMode(), ev)); got != "\x1b[<16;4;5M" {
		t.Fatalf("encoded event = %q", got)
	}
}

// Клик по строке состояния — не клик по терминалу: событие в псевдотерминал
// уходить не должно.
func TestTerminalIgnoresClicksOutsideTheScreen(t *testing.T) {
	m := newTerminalTui()
	m.width, m.height = 40, 10
	m.term.screen.Resize(40, 9)
	m.term.screen.Write([]byte("\x1b[?1000h"))
	// Строка состояния и всё, что ниже экрана, — не терминал.
	for _, pt := range [][2]int{{0, m.height - 1}, {0, 20}, {5, 9}} {
		if m.inTerminalScreen(pt[0], pt[1]) {
			t.Errorf("(%d,%d) must be outside the terminal screen", pt[0], pt[1])
		}
	}
	// Углы экрана (9 строк: 0..8) — внутри.
	if !m.inTerminalScreen(0, 0) || !m.inTerminalScreen(39, 8) {
		t.Error("the corners of the terminal screen must be inside it")
	}
}

// Событие не должно уходить, когда псевдотерминала нет: окно после неудачного
// запуска оболочки остаётся живым, и клик по нему не должен падать.
func TestForwardMouseWithoutSession(t *testing.T) {
	m := newTerminalTui()
	m.term.screen.Write([]byte("\x1b[?1000h"))
	m.term.term = nil
	m.forwardMouse(vt.MouseEvent{Action: vt.MousePress, Col: 1, Row: 1}) // не должно падать
}
