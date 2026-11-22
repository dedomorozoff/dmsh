package cli

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

func TestSubmitToLLMEchoesUserInput(t *testing.T) {
	m := newTestTui()
	m.s.engine = &captureEngine{tokens: []string{`{"command":"ls","explanation":"list"}`}}
	m.input = "ls -la"
	m.cursorPos = 6
	mm, _ := m.submitToLLM(m.input)
	m = mm.(tuiModel)
	if m.streaming != true || m.streamed != false {
		t.Fatalf("after submit: streaming=%v streamed=%v, want true/false", m.streaming, m.streamed)
	}
	if !strings.Contains(m.content, "> ls -la") {
		t.Fatalf("user request must be echoed into the transcript:\n%s", m.content)
	}
}

func TestThinkingIndicatorWhileStreaming(t *testing.T) {
	m := newTestTui()
	m.width = 120
	m.height = 10
	m.streaming = true
	m.streamed = false
	out := m.render()
	if !strings.Contains(out, "thinking") {
		t.Fatalf("streaming should show a thinking indicator:\n%s", out)
	}
	m.streamed = true
	if out2 := m.render(); !strings.Contains(out2, "thinking") {
		t.Fatalf("thinking indicator must persist while tokens are streaming:\n%s", out2)
	}
	m.streaming = false
	if out3 := m.render(); strings.Contains(out3, "thinking") {
		t.Fatalf("thinking indicator must vanish once streaming ends:\n%s", out3)
	}
}

func TestMenuShowsDescriptions(t *testing.T) {
	m := newTestTui()
	m.width = 120
	m.height = 10
	m.input = "/c"
	out := m.render()
	if !strings.Contains(out, "/cd") || !strings.Contains(out, "/clear") {
		t.Fatalf("menu should list candidates:\n%s", out)
	}
	if !strings.Contains(out, "—") {
		t.Fatalf("menu should show descriptions:\n%s", out)
	}
}

func TestMenuArrowsSelect(t *testing.T) {
	m := newTestTui()
	m.input = "/c"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "/clear" {
		t.Fatalf("down selects first match, input = %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "/cd" {
		t.Fatalf("second down cycles to next match, input = %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "/clear" {
		t.Fatalf("up should step up in the menu, input = %q", m.input)
	}
}

func TestEnterRunsSelectedMenuCommand(t *testing.T) {
	m := newTestTui()
	m.input = "/p"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "/pwd" {
		t.Fatalf("down should select /pwd, input = %q", m.input)
	}
	mm, cmd := m.handleEnter()
	if cmd != nil {
		t.Fatal("menu /pwd must not submit to the LLM")
	}
	m = mm.(tuiModel)
	wd, _ := os.Getwd()
	if !strings.Contains(m.content, wd) {
		t.Fatalf("enter should run the selected command, content:\n%s", m.content)
	}
}

func TestEnterCompletesUniqueMenuMatch(t *testing.T) {
	m := newTestTui()
	m.input = "/hi"
	m.cursorPos = 3
	// "/hi" is an unambiguous prefix of "/history": Enter must route to the
	// slash command, not to the LLM.
	mm, cmd := m.handleEnter()
	m = mm.(tuiModel)
	if cmd != nil {
		t.Fatal("unique /history match must not submit to the LLM")
	}
	if m.tabMatches != nil || m.tabIdx != -1 {
		t.Fatalf("enter should clear menu state: matches=%v idx=%d", m.tabMatches, m.tabIdx)
	}
}

func TestCtrlPOpensPalette(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.state != tuiPalette {
		t.Fatalf("ctrl+p should open the command palette, state = %d", m.state)
	}
	if m.input != "" {
		t.Fatalf("palette should start with an empty query, got %q", m.input)
	}
	if len(paletteMatches("")) == 0 {
		t.Fatal("palette should list commands")
	}
}

func TestCtrlPFiltersPalette(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = press(m, keyText("cle"))
	items := paletteMatches(m.input)
	if len(items) != 1 || items[0].name != "/clear" {
		t.Fatalf("query %q matched %v, want only /clear", m.input, items)
	}
	mm, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("palette /clear must not submit to the LLM")
	}
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("palette should close after Enter, state = %d", got.state)
	}
}

func TestPaletteEnterRunsSelected(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = press(m, keyText("help"))
	mm, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("/help must not submit to the LLM")
	}
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("palette should close after running, state = %d", got.state)
	}
	if !strings.Contains(got.content, "dmsh help") {
		t.Fatalf("/help should print the help, content:\n%s", got.content)
	}
}

func TestPaletteArrowsMoveSelection(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.paletteIdx != 0 {
		t.Fatalf("initial selection = %d, want 0", m.paletteIdx)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.paletteIdx != 1 {
		t.Fatalf("down: selection = %d, want 1", m.paletteIdx)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.paletteIdx != 0 {
		t.Fatalf("up: selection = %d, want 0", m.paletteIdx)
	}
	// Clamped at the top, not wrapped around.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.paletteIdx != 0 {
		t.Fatalf("up at the top: selection = %d, want 0 (clamped)", m.paletteIdx)
	}
}

func TestPaletteEscapeRestoresInput(t *testing.T) {
	m := newTestTui()
	m.input = "черновик"
	m.cursorPos = runeSliceLen(m.input)
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.input != "" {
		t.Fatalf("opening the palette should clear the query, got %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("esc: state = %d, want tuiIdle", m.state)
	}
	if m.input != "черновик" {
		t.Fatalf("esc should restore the input line, got %q", m.input)
	}
	if m.cursorPos != runeSliceLen("черновик") {
		t.Fatalf("cursorPos = %d after restore", m.cursorPos)
	}
}

func TestPaletteBackspaceEditsQuery(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = press(m, keyText("h"))
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input != "" {
		t.Fatalf("backspace should erase the query, got %q", m.input)
	}
	// Typing resets the selection so the highlight stays on a real candidate.
	m = press(m, keyText("ex"))
	items := paletteMatches(m.input)
	if m.paletteIdx != 0 || m.paletteIdx >= len(items) {
		t.Fatalf("selection = %d, candidates = %d", m.paletteIdx, len(items))
	}
}

func TestPaletteSwitchesMode(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  config.Mode
		label string
	}{
		{"mode:ai", config.ModeAI, "ai"},
		{"mode:help", config.ModeHelp, "help"},
		{"mode:shell", config.ModeShell, "terminal"},
	} {
		m := newTestTui()
		m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
		m = press(m, keyText(tc.query))
		mm, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		got := mm.(tuiModel)
		if got.rf.cfg.Mode != tc.want {
			t.Fatalf("query %q: mode = %q, want %q", tc.query, got.rf.cfg.Mode, tc.want)
		}
		if got.modeLabel != tc.label {
			t.Fatalf("query %q: modeLabel = %q, want %q", tc.query, got.modeLabel, tc.label)
		}
		if got.s.cfg.Mode != tc.want {
			t.Fatalf("query %q: session mode = %q, want %q", tc.query, got.s.cfg.Mode, tc.want)
		}
	}
}

func TestPaletteRowsFitWidth(t *testing.T) {
	m := newTestTui()
	m.width = 40
	m.height = 24
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for _, row := range m.paletteModal().rows {
		if w := displayWidth(row); w > m.width {
			t.Fatalf("palette row is %d wide, want <= %d: %q", w, m.width, row)
		}
	}
}

func TestPaletteNoMatchKeepsPaletteOpen(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = press(m, keyText("zzz"))
	mm, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.state != tuiPalette {
		t.Fatalf("no match should keep the palette open, state = %d", got.state)
	}
	if cmd != nil {
		t.Fatal("no match must not run anything")
	}
	if !strings.Contains(strings.Join(m.paletteModal().rows, "\n"), "no matching command") {
		t.Fatal("empty result should say so")
	}
}

func TestCtrlQQuits(t *testing.T) {
	m := newTestTui()
	mm, cmd := m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q: expected a quit command")
	}
	got := mm.(tuiModel)
	if !strings.Contains(got.content, "bye!") {
		t.Fatalf("ctrl+q should print bye!, content: %q", got.content)
	}
}

func TestCtrlQQuitsWhileStreaming(t *testing.T) {
	m := newTestTui()
	m.streaming = true
	m.state = tuiStreaming
	stopped := false
	m.streamCancel = func() { stopped = true }
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q during streaming: expected a quit command")
	}
	if !stopped {
		t.Fatal("ctrl+q must cancel the in-flight inference")
	}
}

func TestF1WorksInEveryState(t *testing.T) {
	states := []tuiState{tuiIdle, tuiModelMenu, tuiSearch, tuiConfirming, tuiQuestion, tuiStreaming, tuiPalette, tuiSettings, tuiSetup, tuiModeMenu, tuiRunning}
	// F1 reaches us as tea.KeyF1; some terminals add shift, so both
	// spellings must be handled.
	keys := []tea.KeyPressMsg{
		{Code: tea.KeyF1},
		{Code: tea.KeyF1, Mod: tea.ModShift},
	}
	for _, st := range states {
		for _, key := range keys {
			m := newTestTui()
			m.state = st
			mm, cmd := m.handleKey(key)
			if cmd != nil {
				t.Fatalf("state %d, key %q: F1 must not return a command", st, key.Keystroke())
			}
			got := mm.(tuiModel)
			if !strings.Contains(got.content, "dmsh help") {
				t.Fatalf("state %d, key %q: F1 should show help, content:\n%s", st, key.Keystroke(), got.content)
			}
		}
	}
}

func TestShiftTabCyclesModes(t *testing.T) {
	want := []config.Mode{config.ModeHelp, config.ModeShell, config.ModeAI, config.ModeHelp}
	m := newTestTui()
	if m.rf.cfg.Mode != config.ModeAI {
		t.Fatalf("test setup: mode = %q, want ai", m.rf.cfg.Mode)
	}
	for i, w := range want {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if m.rf.cfg.Mode != w {
			t.Fatalf("shift+tab #%d: mode = %q, want %q", i+1, m.rf.cfg.Mode, w)
		}
		if m.s.cfg.Mode != w {
			t.Fatalf("shift+tab #%d: session mode = %q, want %q", i+1, m.s.cfg.Mode, w)
		}
		if m.modeLabel != modeLabel(w) {
			t.Fatalf("shift+tab #%d: modeLabel = %q, want %q", i+1, m.modeLabel, modeLabel(w))
		}
	}
}

func TestTabStillCompletesAfterShiftTabSupport(t *testing.T) {
	m := newTestTui()
	m.input = "/hi"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input != "/history" {
		t.Fatalf("plain tab must still complete, input = %q", m.input)
	}
	if m.rf.cfg.Mode != config.ModeAI {
		t.Fatalf("plain tab must not change the mode, got %q", m.rf.cfg.Mode)
	}
}

func TestSlashNumericModesAreGone(t *testing.T) {
	for _, gone := range []string{"/1", "/2", "/3", "/mode 1", "/mode 2", "/mode 3"} {
		if IsModeCommand(gone) {
			t.Errorf("%q must no longer be a mode command", gone)
		}
		if ParseModeCommand(gone) != "" {
			t.Errorf("%q must not resolve to a mode", gone)
		}
		for _, c := range slashCommands {
			if c == gone {
				t.Errorf("%q must be removed from the slash menu", gone)
			}
		}
	}
	for _, kept := range []string{"/mode", "/mode ai", "/mode help", "/mode shell"} {
		if !IsModeCommand(kept) {
			t.Errorf("%q should still be a mode command", kept)
		}
	}
}

func TestShiftTabHintsForTerminalMode(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // -> help
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // -> shell
	if m.rf.cfg.Mode != config.ModeShell {
		t.Fatalf("mode = %q, want shell", m.rf.cfg.Mode)
	}
	if !strings.Contains(m.content, "terminal") {
		t.Fatalf("terminal mode should say how to open a shell:\n%s", m.content)
	}
}

func TestHelpModeHintDisablesRunCommand(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // -> help
	if !strings.Contains(m.content, "run_command is disabled") {
		t.Fatalf("help mode should warn that run_command is off:\n%s", m.content)
	}
}
