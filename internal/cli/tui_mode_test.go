package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/vt"
)

func TestHistorySearch(t *testing.T) {
	m := newTestTui()
	m.history = []string{"ls", "git status", "clear"}
	mm, _ := m.startSearch()
	m = mm.(tuiModel)
	if m.state != tuiSearch || m.input != "clear" {
		t.Fatalf("after ctrl+r: state=%v input=%q, want search/%q", m.state, m.input, "clear")
	}
	m = press(m, keyText("c"))
	if m.input != "clear" {
		t.Fatalf("narrowing query: input = %q, want %q", m.input, "clear")
	}
	m = press(m, keyText("r"))
	if m.input != "" {
		t.Fatalf("query 'cr' has no match, input should be empty, got %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input != "git status" {
		t.Fatalf("ctrl+r should step to older match, input = %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != tuiIdle || m.input != "git status" {
		t.Fatalf("enter should accept the search result, state=%v input=%q", m.state, m.input)
	}
}

// Режим терминала никогда не обращается к модели: команды уходят в
// псевдотерминал внутри окна, а не в LLM.
func TestShellModeNoLLM(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.input = "pwd date"
	m.cursorPos = 8
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)
	if m.streaming || m.state == tuiStreaming {
		t.Fatal("shell mode must not enter streaming/LLM state")
	}
	if m.s.lastInput == "pwd date" {
		t.Fatalf("shell mode sent the command to the LLM: %q", m.s.lastInput)
	}
}

func TestTerminalModeEmptyLineOpensShell(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	mm, _ := m.handleEnter()
	m = mm.(tuiModel)
	if m.streaming || m.state == tuiStreaming {
		t.Fatal("opening a shell must not touch the LLM")
	}
	if m.rf.cfg.Mode != config.ModeShell {
		t.Fatalf("mode = %q, want shell", m.rf.cfg.Mode)
	}
	// Без настроенной оболочки терминал не поднимается — и об этом сказано
	// прямо, а не тихо.
	if !strings.Contains(m.content, "no shell configured") {
		t.Fatalf("empty line in terminal mode should try to open a shell:\n%s", m.content)
	}
}

// Вывод встроенного терминала рисуется на месте, а не попадает в
// транскрипт: пользователь смотрит на экран оболочки.
func TestTerminalOutputGoesToScreen(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 40, 6
	m.state = tuiTerminal
	m.term = &terminalPane{screen: vt.NewScreen(40, 5)}

	mm, _ := m.Update(terminalOutputMsg{data: []byte("PS D:\\dmsh> echo dmsh-term-marker\r\ndmsh-term-marker")})
	got := mm.(tuiModel)
	rows := got.render()
	if !strings.Contains(rows, "dmsh-term-marker") {
		t.Fatalf("terminal output must be visible on screen, frame:\n%s", rows)
	}
	if strings.Contains(got.content, "dmsh-term-marker") {
		t.Fatalf("terminal output must not be mixed into the transcript:\n%s", got.content)
	}
}

// Вставка в режиме терминала уходит в оболочку, а не в строку ввода:
// текст не должен возвращаться в промпт после выхода из терминала.
func TestTerminalPasteDoesNotReachThePrompt(t *testing.T) {
	m := newTestTui()
	m.state = tuiTerminal
	m.term = &terminalPane{screen: vt.NewScreen(40, 5)}
	mm, _ := m.Update(tea.PasteMsg{Content: "echo привет\r\n"})
	got := mm.(tuiModel)
	if got.input != "" {
		t.Fatalf("terminal paste must not land in the prompt: %q", got.input)
	}
	if got.cursorPos != 0 {
		t.Fatalf("cursor = %d, want it left where it was", got.cursorPos)
	}
}

// Выход оболочки возвращает ввод в строку: оставаться в режиме terminal
// без терминала нельзя.
func TestTerminalExitReportsAndLeavesMode(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.s.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.state = tuiTerminal

	mm, _ := m.Update(terminalExitMsg{code: 0})
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("state = %d after the shell exited, want tuiIdle", got.state)
	}
	if got.rf.cfg.Mode != config.ModeAI {
		t.Fatalf("mode = %q after the shell exited, want ai", got.rf.cfg.Mode)
	}
	if !strings.Contains(got.content, "shell exited") {
		t.Fatalf("leaving the terminal should be reported:\n%s", got.content)
	}
}

// /model в TUI печатает те же строки, что и REPL: общий modelInfoLines.
func TestShowModelUsesSharedLines(t *testing.T) {
	m := newTestTui()
	m.s.provider = config.ProviderPollinations
	m.s.cfg.RemoteModel = "mistral"
	m.s.cfg.RemoteBaseURL = "https://example.test/v1"
	m.showModel()
	for _, want := range []string{"pollinations", "mistral", "https://example.test/v1"} {
		if !strings.Contains(m.content, want) {
			t.Fatalf("TUI /model should show %q:\n%s", want, m.content)
		}
	}
}

// Палитра — единая точка входа: всё, что она показывает, должно
// существовать как слэш-команда (или как запись режима).
func TestPaletteEntriesAllResolvable(t *testing.T) {
	for _, it := range paletteItems {
		cmd := it.paletteCommand()
		if it.key != "" {
			// Записи режима идут через /mode.
			if !IsModeCommand(cmd) {
				t.Errorf("palette entry %q maps to %q, which is not a mode command", it.name, cmd)
			}
			if ParseModeCommand(cmd) == "" {
				t.Errorf("palette entry %q maps to %q, which resolves to no mode", it.name, cmd)
			}
			continue
		}
		known := false
		for _, c := range slashCommands {
			if c == cmd {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("palette entry %q maps to %q, which is not in slashCommands", it.name, cmd)
		}
		if slashDesc[cmd] == "" {
			t.Errorf("palette entry %q has no description in slashDesc", cmd)
		}
	}
}

// Статусная строка и F1 должны быть согласованы с тем, что реально
// работает: иначе подсказки врут.
func TestStatuslineHintsMatchRealBindings(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyF1},
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'q', Mod: tea.ModCtrl},
	} {
		m := newTestTui()
		mm, cmd := m.handleKey(key)
		got := mm.(tuiModel)
		switch key.Keystroke() {
		case "f1":
			if !strings.Contains(got.content, "dmsh help") {
				t.Errorf("statusline advertises F1 but it does not show help")
			}
		case "ctrl+q":
			if cmd == nil {
				t.Errorf("statusline advertises Ctrl+Q but it does not quit")
			}
		case "ctrl+p":
			if got.state != tuiPalette {
				t.Errorf("statusline advertises Ctrl+P but it does not open the palette")
			}
		}
	}
}

// /shell переводит сессию в режим терминала: иначе следующий ввод снова
// уйдёт в модель.
func TestShellSlashCommandSwitchesToTerminalMode(t *testing.T) {
	m := newTestTui()
	mm, _ := m.handleSlash("/shell")
	got := mm.(tuiModel)
	if got.rf.cfg.Mode != config.ModeShell {
		t.Fatalf("mode = %q, want shell so the session matches the opened terminal", got.rf.cfg.Mode)
	}
	if got.s.cfg.Mode != config.ModeShell {
		t.Fatalf("session mode = %q, want shell", got.s.cfg.Mode)
	}
	// Без настроенной оболочки терминал не поднимается, и это должно быть
	// сказано прямо, а не тихо проигнорировано.
	if !strings.Contains(got.content, "no shell configured") {
		t.Fatalf("a missing shell should be reported:\n%s", got.content)
	}
}

func TestLongLineWrappingKeepsStatus(t *testing.T) {
	m := newTestTui()
	m.width = 30
	m.height = 8
	m.content = "привет, " + strings.Repeat("абвгд", 40) + "\n"
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) != 8 {
		t.Fatalf("render produced %d rows, want 8", len(lines))
	}
	if lines[7] != fitWidth(m.statusline(), m.width) {
		t.Fatalf("last row is not the (fitted) status line:\n%s", out)
	}
	for i, l := range lines {
		if dw := displayWidth(l); dw > m.width {
			t.Fatalf("row %d exceeds width %d (%d): %q", i, m.width, dw, l)
		}
	}
}

func TestFitWidthTruncates(t *testing.T) {
	if got := fitWidth("short", 20); got != "short" {
		t.Fatalf("fitWidth must not touch short strings, got %q", got)
	}
	long := strings.Repeat("x", 100)
	for _, w := range []int{4, 20, 80} {
		if got := displayWidth(fitWidth(long, w)); got != w {
			t.Fatalf("fitWidth(%q, %d): display width = %d, want %d", long, w, got, w)
		}
	}
}

func TestStatuslineShowsRealModel(t *testing.T) {
	// Real scenario: rf.cfg has no model path; newSession resolves it into
	// its own cfg copy, so the status line must read it from there.
	m := NewTuiModel(
		&rootFlags{cfg: config.Config{Mode: config.ModeAI}},
		&session{cfg: config.Config{Mode: config.ModeAI, ModelPath: `D:\models\q4.gguf`}},
	).(tuiModel)
	if m.rf.cfg.ModelPath != "" {
		t.Fatal("test setup: rf.cfg.ModelPath should be empty")
	}
	s := ansi.Strip(m.statusline())
	if strings.Contains(s, "model:none") {
		t.Fatalf("statusline reports no model despite session.cfg.ModelPath set: %s", s)
	}
	if !strings.Contains(s, "model:q4.gguf") {
		t.Fatalf("statusline should show the session model name: %s", s)
	}
	if strings.Contains(s, "q:quit") {
		t.Fatalf("statusline must not advertise a non-existent q:quit binding: %s", s)
	}
	if !strings.Contains(s, "F1") {
		t.Fatalf("statusline should hint the F1 help: %s", s)
	}
	if !strings.Contains(s, "Ctrl+P") {
		t.Fatalf("statusline should hint the command palette: %s", s)
	}
	if strings.Contains(s, "ctrl-d") || strings.Contains(s, "ctrl-p") || strings.Contains(s, "/help") || strings.Contains(s, "!cmd") || strings.Contains(s, "ctrl-r") {
		t.Fatalf("statusline should stay minimal (no ctrl-d/ctrl-p/!cmd/ctrl-r): %s", s)
	}
}

// captureEngine — минимальная заглушка llm.Engine для тестов TUI-потока.
