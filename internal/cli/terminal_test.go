package cli

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
)

// newTerminalTui — модель с готовым экраном терминала, но без живого
// псевдотерминала: для проверок раскладки и клавиш он не нужен, а
// поднимать настоящую оболочку в каждом тесте дорого.
func newTerminalTui() tuiModel {
	m := newTestTui()
	m.width, m.height = 80, 24
	m.rf.cfg.Mode = config.ModeShell
	m.s.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.state = tuiTerminal
	m.term = newTerminalPane(nil, 80, 23)
	return m
}

// Сдвиг окна списка держит выделение на экране: без него палитра из
// двадцати пунктов показывала первые десять и писала «+8 more».
func TestScrollWindowFollowsSelection(t *testing.T) {
	const size = 10
	start, end := scrollWindow(0, 20, size)
	if start != 0 || end != size {
		t.Fatalf("first window = [%d,%d), want [0,%d)", start, end, size)
	}
	start, end = scrollWindow(15, 20, size)
	if start > 15 || end <= 15 {
		t.Fatalf("window [%d,%d) hides the selection 15", start, end)
	}
	start, end = scrollWindow(19, 20, size)
	if end != 20 {
		t.Fatalf("last window = [%d,%d), want the list to end at 20", start, end)
	}
	start, end = scrollWindow(2, 3, size)
	if start != 0 || end != 3 {
		t.Fatalf("short list = [%d,%d), want [0,3)", start, end)
	}
}

func TestPaletteListScrollsWithSelection(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 24
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	items := paletteMatches(m.input)
	if len(items) <= 10 {
		t.Fatalf("need a list longer than the window for the test, got %d items", len(items))
	}
	rows := strings.Join(m.paletteModal().rows, "\n")
	if strings.Contains(rows, "more)") {
		t.Fatalf("the truncated marker must be gone:\n%s", rows)
	}
	if !strings.Contains(rows, items[0].paletteDisplay()) {
		t.Fatalf("first item missing:\n%s", rows)
	}
	if strings.Contains(rows, items[len(items)-1].paletteDisplay()) {
		t.Fatalf("last item is not reachable at the top of the list:\n%s", rows)
	}

	// Доводим выделение до последнего пункта: он обязан появиться.
	for m.paletteIdx < len(items)-1 {
		m.paletteMove(1)
	}
	rows = strings.Join(m.paletteModal().rows, "\n")
	if !strings.Contains(rows, items[len(items)-1].paletteDisplay()) {
		t.Fatalf("selection at the end of the list is not visible:\n%s", rows)
	}
	if !strings.Contains(rows, "of "+strconv.Itoa(len(items))) {
		t.Fatalf("the header should show the position in the list:\n%s", rows)
	}
}

// Меню слэш-команд ограничено восемью строками и тоже должно листаться.
func TestSlashMenuWindowFollowsSelection(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 120, 40
	m.input = "/"
	m.cursorPos = 1
	menu := m.menuList()
	if len(menu) <= 8 {
		t.Fatalf("need more than 8 commands for the test, got %d", len(menu))
	}
	for i := 0; i < 10; i++ {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	out := m.render()
	if !strings.Contains(out, menu[m.tabIdx]) {
		t.Fatalf("selected command %q is not visible after scrolling:\n%s", menu[m.tabIdx], out)
	}
	if strings.Contains(out, "(+") {
		t.Fatalf("the truncated marker must be gone:\n%s", out)
	}
}

func TestTerminalKeyBytes(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "\r"},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, "\t"},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, "\x7f"},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}, "\x1b"},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, "\x1b[B"},
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, "\x1b[H"},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, "\x1b[3~"},
		{"text", tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{"cyrillic", tea.KeyPressMsg{Code: 'п', Text: "п"}, "п"},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "\x03"},
		{"ctrl+l", tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}, "\x0c"},
		{"unknown", tea.KeyPressMsg{Code: tea.KeyF20}, ""},
	}
	for _, tc := range cases {
		got := string(terminalKeyBytes(tc.msg))
		if got != tc.want {
			t.Errorf("%s: bytes = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Shift+Tab в терминале возвращает в обычный режим: иначе из него не
// выйти, не закрыв оболочку.
func TestTerminalShiftTabLeavesTerminal(t *testing.T) {
	m := newTerminalTui()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("state = %d after shift+tab, want tuiIdle", got.state)
	}
	if got.rf.cfg.Mode == config.ModeShell {
		t.Fatalf("mode = %q, want the terminal mode to be left", got.rf.cfg.Mode)
	}
	if got.term != nil {
		t.Fatal("the pseudo-terminal must be closed when leaving the mode")
	}
}

// Печатные клавиши уходят в оболочку, а не в строку ввода dmsh.
func TestTerminalKeysGoToTheShell(t *testing.T) {
	m := newTerminalTui()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got := mm.(tuiModel)
	if got.input != "" {
		t.Fatalf("terminal keys must not edit the dmsh prompt, got %q", got.input)
	}
}

// Окно терминала занимает весь кадр, кроме статусной строки.
func TestTerminalFrameKeepsStatusLine(t *testing.T) {
	m := newTerminalTui()
	m.term.screen.WriteString("PS D:\\dmsh> ls")
	frame := strings.Split(m.render(), "\n")
	if len(frame) != m.height {
		t.Fatalf("frame has %d rows, want %d", len(frame), m.height)
	}
	if frame[0] != "PS D:\\dmsh> ls" {
		t.Fatalf("row 0 = %q, want the terminal screen", frame[0])
	}
	if frame[len(frame)-1] != fitWidth(m.statusline(), m.width) {
		t.Fatalf("last row is not the status line:\n%s", strings.Join(frame, "\n"))
	}
}

// Полный круг: оболочка в окне, ввод в неё, выход из неё.
func TestEmbeddedTerminalRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns an interactive shell")
	}
	m := newTestTui()
	m.width, m.height = 80, 24
	m.rf.cfg.Shell = testShell()

	mm, cmd := m.startTerminal()
	m = mm
	if m.term == nil {
		t.Fatalf("terminal did not start: %s", m.content)
	}
	defer m.stopTerminal("")

	mark := "dmsh-embedded-" + time.Now().Format("150405.000000")
	for _, line := range []string{mark, "exit"} {
		if _, err := m.term.term.Write([]byte(executor.TerminalScript(line))); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	if cmd == nil {
		t.Fatal("startTerminal must return the output pump command")
	}
	// Ждём либо выхода оболочки, либо появления маркера на экране.
	deadline := time.After(60 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("the shell never produced the marker")
		default:
		}
		msg := cmd()
		switch v := msg.(type) {
		case terminalOutputMsg:
			_, _ = m.term.screen.Write(v.data)
			if strings.Contains(strings.Join(m.term.screen.Text(), "\n"), mark) {
				return
			}
		case terminalExitMsg:
			if v.err != nil {
				t.Fatalf("shell failed: %v", v.err)
			}
			return
		default:
			t.Fatalf("unexpected message %T", msg)
		}
	}
}

// Вывод терминала продолжает приходить под открытым окном: иначе первое же
// открытие палитры навсегда останавливало бы оболочку на экране.
func TestTerminalOutputKeepsPumpingUnderModal(t *testing.T) {
	m := newTerminalTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.state != tuiPalette {
		t.Fatalf("state = %d, want tuiPalette", m.state)
	}
	mm, cmd := m.Update(terminalOutputMsg{data: []byte("under the modal")})
	got := mm.(tuiModel)
	if cmd == nil {
		t.Fatal("the output pump must stay armed while a modal is open")
	}
	if !strings.Contains(strings.Join(got.term.screen.Text(), "\n"), "under the modal") {
		t.Fatalf("output must still reach the terminal screen:\n%s", strings.Join(got.term.screen.Text(), "\n"))
	}
	// Курсор при этом остаётся в окне, а не уезжает под него.
	if v := got.View(); v.Cursor == nil {
		t.Fatal("no cursor while the palette is open")
	}
}

// F1 открывает справку и из терминала: иначе подсказка была бы из
// половины экранов недостижимой.
func TestHelpWorksInsideTerminal(t *testing.T) {
	m := newTerminalTui()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyF1})
	got := mm.(tuiModel)
	if !strings.Contains(got.content, "dmsh help") {
		t.Fatalf("F1 in terminal mode should show help:\n%s", got.content)
	}
	if got.term != nil {
		t.Fatal("the terminal must be closed so the help is readable")
	}
	if !strings.Contains(got.content, "closed") {
		t.Fatalf("closing the terminal should be reported:\n%s", got.content)
	}
}
