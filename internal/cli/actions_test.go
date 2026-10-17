package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// doneCmdTui — сессия, где команда только что выполнилась: панель под ответом
// должна быть на месте.
func doneCmdTui(t *testing.T, cmd string) tuiModel {
	t.Helper()
	m := newTestTui()
	m.width, m.height = 80, 20
	m.rf.cfg.Mode = config.ModeAI
	m.actionCmd = cmd
	return m
}

// Панель появляется только после выполненной команды: перед первым запросом
// предлагать «повторить» бессмысленно.
func TestActionBarOnlyAfterCommand(t *testing.T) {
	empty := newTestTui()
	empty.width, empty.height = 80, 20
	if empty.showActionBar() {
		t.Error("no command has run yet, so there is nothing to offer")
	}
	if len(empty.actionBarRegions()) != 0 {
		t.Error("an absent panel must not be clickable")
	}

	m := doneCmdTui(t, "ls -la")
	if !m.showActionBar() {
		t.Error("the panel must show after a command")
	}
	if len(m.actionBarRegions()) != len(actionBarItems) {
		t.Fatalf("the panel has %d buttons, want %d", len(m.actionBarRegions()), len(actionBarItems))
	}
}

// Кнопка закрывает свой текст на экране: иначе клик уедет на соседнюю.
func TestActionBarButtonsMatchRenderedText(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	regions := m.actionBarRegions()
	for i, item := range actionBarItems {
		if i >= len(regions) {
			t.Fatalf("no region for the %q button", item.label)
		}
		row := frameRow(t, m, regions[i].y)
		if r := regions[i]; r.x1 > len(row) {
			t.Fatalf("region %+v does not fit the row", r)
		} else if got := strings.TrimSpace(row[r.x0:r.x1]); got != "[ "+item.label+" ]" {
			t.Errorf("button %d covers %q, want %q", i, got, "[ "+item.label+" ]")
		}
	}
}

// retry делает ровно то же, что /retry: панель — короткий путь, а не второй
// интерфейс.
func TestActionBarRetryRunsRetry(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	m.s.lastInput = "show me the files"

	mm, _ := m.runActionBar(0)
	got := mm.(tuiModel)
	if got.actionCmd != "" {
		t.Error("a new request must retire the panel")
	}
	// /retry сам по себе печатает запрос в транскрипт — значит, он ушёл.
	if !strings.Contains(got.content, "show me the files") {
		t.Errorf("retry must resend the last request:\n%s", got.content)
	}
}

// explain — это запрос модели, а не команда оболочки: он подставляется в
// строку ввода и ждёт отправки.
func TestActionBarExplainFillsInput(t *testing.T) {
	m := doneCmdTui(t, "rm -rf /tmp/x")
	mm, cmd := m.runActionBar(1)
	got := mm.(tuiModel)
	if cmd != nil {
		t.Error("explain must not send anything by itself")
	}
	if !strings.Contains(got.input, "rm -rf /tmp/x") {
		t.Errorf("the prompt must mention the command: %q", got.input)
	}
	if got.state != tuiIdle {
		t.Errorf("state = %v, want the prompt to stay editable", got.state)
	}
}

// shell открывает встроенный терминал.
func TestActionBarShellOpensTerminal(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	m.rf.cfg.Shell = testShell()
	mm, _ := m.runActionBar(2)
	got := mm.(tuiModel)
	if got.state != tuiTerminal {
		t.Fatalf("state = %v, want the terminal", got.state)
	}
	if got.rf.cfg.Mode != config.ModeShell {
		t.Errorf("mode = %q, want shell so the session matches the opened terminal", got.rf.cfg.Mode)
	}
}

// Клик по кнопке равносилен её команде: отдельный путь для мыши разошёлся бы
// с клавиатурой.
func TestActionBarClickEqualsCommand(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	m.s.lastInput = "show me the files"
	r := clickRegionFor(t, m, clickRetry, 0)
	got := click(m, r.x0, r.y)
	if !strings.Contains(got.content, "show me the files") {
		t.Errorf("clicking retry must resend the request:\n%s", got.content)
	}
}

// Пока идёт команда или стримится, панели нет: она предлагает то, что уже не
// актуально.
func TestActionBarHiddenWhileBusy(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	for _, busy := range []func(*tuiModel){
		func(x *tuiModel) { x.running = true },
		func(x *tuiModel) { x.streaming = true },
		func(x *tuiModel) { x.state = tuiRunning },
	} {
		b := doneCmdTui(t, "ls -la")
		busy(&b)
		if b.showActionBar() {
			t.Errorf("the panel must hide while the session is busy (state=%d running=%v)", b.state, b.running)
		}
	}
	_ = m
}

// Новый запрос убирает панель: она относится к предыдущей команде.
func TestNewRequestRetiresActionBar(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	mm, _ := m.submitToLLMEcho("what next?", "what next?")
	got := mm.(tuiModel)
	if got.actionCmd != "" {
		t.Error("a new request must clear the panel")
	}
	if got.showActionBar() {
		t.Error("the panel must not survive a new request")
	}
}

// Панель занимает строку рядом со строкой ввода и не ломает раскладку кадра.
func TestActionBarKeepsFrame(t *testing.T) {
	m := doneCmdTui(t, "ls -la")
	rows := strings.Split(m.render(), "\n")
	if len(rows) != m.height {
		t.Fatalf("frame has %d rows, want %d", len(rows), m.height)
	}
	// Сравниваем с обесцвеченным текстом: в кадре та же строка, но с цветами.
	bar := stripANSI(m.actionBarRow())
	found := false
	for _, row := range rows {
		if strings.Contains(stripANSI(row), bar) {
			found = true
		}
		if w := displayWidth(row); w > m.width {
			t.Fatalf("row is %d wide, want <= %d:\n%s", w, m.width, row)
		}
	}
	if !found {
		t.Fatalf("the panel must be drawn:\n%s", strings.Join(rows, "\n"))
	}
}

func stripANSI(s string) string { return ansi.Strip(s) }
