package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

func newTestTui() tuiModel {
	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI, ModelPath: "/models/q4.gguf"}}
	// Движок задаётся сразу: handleEnter и autoCorrect запускают стрим в
	// фоне, и без него тестовая горутина падает на nil после возврата.
	return NewTuiModel(rf, &session{cfg: rf.cfg, engine: &captureEngine{}}).(tuiModel)
}

func press(m tuiModel, msg tea.KeyPressMsg) tuiModel {
	mm, _ := m.handleKey(msg)
	return mm.(tuiModel)
}

func keyText(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg{Text: s, Code: r}
}

func TestHandleKeyTypesCyrillic(t *testing.T) {
	m := newTestTui()
	m = press(m, keyText("п"))
	m = press(m, keyText("р"))
	m = press(m, keyText("и"))
	m = press(m, keyText("в"))
	m = press(m, keyText("е"))
	m = press(m, keyText("т"))
	if m.input != "привет" {
		t.Fatalf("input = %q, want %q", m.input, "привет")
	}
	if m.cursorPos != 6 {
		t.Fatalf("cursorPos = %d, want 6", m.cursorPos)
	}
}

func TestHandleKeyShiftLetter(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Text: "П", Code: 'п', Mod: tea.ModShift})
	if m.input != "П" {
		t.Fatalf("input = %q, want %q", m.input, "П")
	}
}

func TestHandleKeyCtrlComboIsNotText(t *testing.T) {
	m := newTestTui()
	m.input = "hello"
	m.cursorPos = 5
	// ctrl+a with an associated-text payload must act as a command, not insert text.
	m = press(m, tea.KeyPressMsg{Text: "a", Code: 'a', Mod: tea.ModCtrl})
	if m.input != "hello" {
		t.Fatalf("ctrl+a inserted text: input = %q", m.input)
	}
	if m.cursorPos != 0 {
		t.Fatalf("ctrl+a cursorPos = %d, want 0", m.cursorPos)
	}
}

func TestHandleKeyCtrlE(t *testing.T) {
	m := newTestTui()
	m.input = "привет"
	m.cursorPos = 0
	m = press(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.cursorPos != 6 {
		t.Fatalf("ctrl+e cursorPos = %d, want 6", m.cursorPos)
	}
}

func TestBackspaceRuneSafe(t *testing.T) {
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeyBackspace, Text: ""},      // DEL 0x7f
		{Code: 'h', Mod: tea.ModCtrl, Text: ""}, // Windows conhost BS (ctrl+h)
	} {
		m := newTestTui()
		m.input = "привет"
		m.cursorPos = 6
		m = press(m, msg)
		if m.input != "приве" {
			t.Fatalf("backspace via %q: input = %q, want %q", msg.Keystroke(), m.input, "приве")
		}
		if m.cursorPos != 5 {
			t.Fatalf("cursorPos = %d, want 5", m.cursorPos)
		}
	}
}

func TestDeleteForwardRuneSafe(t *testing.T) {
	m := newTestTui()
	m.input = "абв"
	m.cursorPos = 1
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDelete, Text: ""})
	if m.input != "ав" {
		t.Fatalf("delete forward: input = %q, want %q", m.input, "ав")
	}
	if m.cursorPos != 1 {
		t.Fatalf("cursorPos = %d, want 1", m.cursorPos)
	}
}

func TestInsertAtMiddle(t *testing.T) {
	m := newTestTui()
	m.input = "ав"
	m.cursorPos = 1
	m = press(m, keyText("б"))
	if m.input != "абв" {
		t.Fatalf("insert at middle: input = %q, want %q", m.input, "абв")
	}
	if m.cursorPos != 2 {
		t.Fatalf("cursorPos = %d, want 2", m.cursorPos)
	}
}

func TestCursorXStripsANSI(t *testing.T) {
	m := newTestTui()
	want := displayWidth(m.buildPrompt())
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("View has no cursor")
	}
	if v.Cursor.X != want {
		t.Fatalf("cursor X = %d, want %d (ANSI escapes must not widen it)", v.Cursor.X, want)
	}
}

func TestCursorXFollowsRunes(t *testing.T) {
	m := newTestTui()
	m.input = "привет"
	m.cursorPos = 3
	v := m.View()
	promptW := displayWidth(m.buildPrompt())
	if v.Cursor.X != promptW+3 {
		t.Fatalf("cursor X = %d, want %d", v.Cursor.X, promptW+3)
	}
}

func TestStatusPinnedAtBottom(t *testing.T) {
	m := newTestTui()
	m.width = 140
	m.height = 6
	m.content = "one\ntwo\nthree\n"
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("render produced %d rows, want %d:\n%s", len(lines), 6, out)
	}
	if lines[5] != m.statusline() {
		t.Fatalf("last row is not the status line:\n%s", out)
	}
	// input row should be just above the status line (content has 3 lines)
	v := m.View()
	if v.Cursor.Y != 3 {
		t.Fatalf("cursor Y = %d, want %d (input just above status)", v.Cursor.Y, 3)
	}
}

func TestStatusPinnedWithLongHistory(t *testing.T) {
	m := newTestTui()
	m.width = 140
	m.height = 6
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString("history line\n")
	}
	m.content = sb.String()
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("render produced %d rows, want %d", len(lines), 6)
	}
	if lines[5] != m.statusline() {
		t.Fatalf("last row is not the status line")
	}
	if !strings.Contains(lines[4], "> ") {
		t.Fatalf("input row (row index 4) does not contain a prompt: %q", lines[4])
	}
	// Only the newest 4 history lines may remain above the input; older ones
	// are scrolled away entirely.
	newest := "history line"
	if lines[0] != newest || lines[1] != newest || lines[2] != newest || lines[3] != newest {
		t.Fatalf("scrolled history rows wrong: %v", lines[:4])
	}
	if v := m.View(); v.Cursor.Y != 4 {
		t.Fatalf("cursor Y = %d, want 4", v.Cursor.Y)
	}
}

func TestPasteInsertsCyrillic(t *testing.T) {
	m := newTestTui()
	mm, _ := m.Update(tea.PasteMsg{Content: "привет мир"})
	m = mm.(tuiModel)
	if m.input != "привет мир" {
		t.Fatalf("input = %q, want %q", m.input, "привет мир")
	}
}

func TestCtrlDExitsOnEmptyLine(t *testing.T) {
	m := newTestTui()
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+d on empty line: expected a quit command")
	}
}

func TestCtrlDDeletesForwardOnText(t *testing.T) {
	m := newTestTui()
	m.input = "абв"
	m.cursorPos = 1
	mm, cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("ctrl+d with text must not quit")
	}
	m = mm.(tuiModel)
	if m.input != "ав" {
		t.Fatalf("ctrl+d delete forward: input = %q, want %q", m.input, "ав")
	}
}

func TestTabCompletesSlashCommand(t *testing.T) {
	m := newTestTui()
	m.input = "/hi"
	m.cursorPos = 3
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input != "/history" {
		t.Fatalf("tab completion: input = %q, want %q", m.input, "/history")
	}
}

func TestTabCyclesAmbiguous(t *testing.T) {
	m := newTestTui()
	m.input = "/c"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input != "/clear" {
		t.Fatalf("first tab should pick the first match, input = %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input != "/cd" {
		t.Fatalf("second tab should cycle to the next match, input = %q", m.input)
	}
	// Typing more input invalidates the completion state.
	m = press(m, keyText("x"))
	if m.input != "/cdx" || m.tabMatches != nil {
		t.Fatalf("typing must reset the completion state: input=%q matches=%v", m.input, m.tabMatches)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input != "/cdx" {
		t.Fatalf("tab after edit should search fresh (no match), input = %q", m.input)
	}
}

func TestAutocompleteMenuShownWhileTypingSlash(t *testing.T) {
	m := newTestTui()
	m.width = 120
	m.height = 10
	m.input = "/c"
	out := m.render()
	if !strings.Contains(out, "/cd") || !strings.Contains(out, "/clear") {
		t.Fatalf("typing / should show the command list, got:\n%s", out)
	}
}

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
	for _, row := range m.paletteRows() {
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
	if !strings.Contains(strings.Join(m.paletteRows(), "\n"), "no matching command") {
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
	states := []tuiState{tuiIdle, tuiModelMenu, tuiSearch, tuiConfirming, tuiQuestion, tuiStreaming, tuiPalette}
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

// Режим терминала никогда не обращается к модели: команда уходит в
// псевдотерминал, а не в LLM.
func TestShellModeNoLLM(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.input = "pwd date"
	m.cursorPos = 8
	mm, cmd := m.handleEnter()
	m = mm.(tuiModel)
	if m.streaming || m.state == tuiStreaming {
		t.Fatal("shell mode must not enter streaming/LLM state")
	}
	if !strings.Contains(m.content, "$ pwd date") {
		t.Fatalf("shell mode should echo the executed command, content:\n%s", m.content)
	}
	// Команда возвращается как terminalRanMsg, а не как запрос к модели.
	if cmd == nil {
		t.Fatal("terminal mode should return a command to run in the pty")
	}
	msg := cmd()
	ran, ok := msg.(terminalRanMsg)
	if !ok {
		t.Fatalf("expected terminalRanMsg, got %T", msg)
	}
	if ran.command != "pwd date" {
		t.Fatalf("command = %q, want %q", ran.command, "pwd date")
	}
}

func TestTerminalModeEmptyLineOpensShell(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.rf.cfg.Shell = testShell()
	mm, cmd := m.handleEnter()
	m = mm.(tuiModel)
	if cmd == nil {
		t.Fatal("empty line in terminal mode should open a shell")
	}
	if m.streaming || m.state == tuiStreaming {
		t.Fatal("opening a shell must not touch the LLM")
	}
}

func TestTerminalModePrintsOutputAndAudits(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.s.cfg.AuditFile = ""
	mm, _ := m.Update(terminalRanMsg{command: "ls -la", output: "file.txt", code: 0})
	got := mm.(tuiModel)
	if !strings.Contains(got.content, "file.txt") {
		t.Fatalf("command output should reach the transcript:\n%s", got.content)
	}
	if got.state != tuiIdle {
		t.Fatalf("state = %d after the command, want tuiIdle", got.state)
	}
	if len(got.s.recent) == 0 || got.s.recent[len(got.s.recent)-1] != "ls -la" {
		t.Fatalf("command should be recorded in recent: %v", got.s.recent)
	}

	mm, _ = m.Update(terminalRanMsg{command: "false", output: "", code: 3})
	got = mm.(tuiModel)
	if !strings.Contains(got.content, "exit 3") {
		t.Fatalf("non-zero exit should be reported:\n%s", got.content)
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
	m := newTestTui()
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyF1},
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'q', Mod: tea.ModCtrl},
	} {
		m = newTestTui()
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

func TestShellSlashCommandSwitchesToTerminalMode(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Shell = testShell()
	mm, cmd := m.handleSlash("/shell")
	got := mm.(tuiModel)
	if got.rf.cfg.Mode != config.ModeShell {
		t.Fatalf("mode = %q, want shell so the session matches the opened shell", got.rf.cfg.Mode)
	}
	if cmd == nil {
		t.Fatal("/shell should return a command to hand the terminal over")
	}
}

// Мусор от оболочки не должен попадать в транскрипт: в выводе PTY остаются
// приглашения, эхо введённых строк и escape-последовательности.
func TestStripShellNoise(t *testing.T) {
	// Реальный вывод PowerShell из-под ConPTY.
	powershell := "\x1b[?9001h\x1b[?25l\x1b[2J\x1b[m\x1b[H" +
		"PS D:\\dmsh> echo dmsh-marker\r\n" +
		"dmsh-marker\r\n" +
		"PS D:\\dmsh> exit\r\n" +
		"\x1b[?9001l"
	if got, want := stripShellNoise(ansi.Strip(powershell), "echo dmsh-marker"), "dmsh-marker"; got != want {
		t.Errorf("powershell output = %q, want %q", got, want)
	}

	bash := "user@host:~$ ls -la\r\ntotal 0\r\ndrwxr-xr-x 2 root root 40 .\r\nuser@host:~$ exit\r\n"
	if got, want := stripShellNoise(ansi.Strip(bash), "ls -la"), "total 0\ndrwxr-xr-x 2 root root 40 ."; got != want {
		t.Errorf("bash output = %q, want %q", got, want)
	}

	// Оболочка не отэхоила команду — выживает всё, кроме чистых приглашений.
	noEcho := "PS D:\\dmsh> \r\nresult line\r\n"
	if got, want := stripShellNoise(noEcho, "whatever"), "result line"; got != want {
		t.Errorf("no-echo output = %q, want %q", got, want)
	}
}

// Реальный вывод терминального режима: команда + выход из оболочки.
func TestStartTerminalCommandStripsShellNoise(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns an interactive shell")
	}
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.rf.cfg.Shell = testShell()

	mm, cmd := m.startTerminalCommand("echo dmsh-tui-marker")
	if cmd == nil {
		t.Fatal("expected a command to run")
	}
	msg, ok := cmd().(terminalRanMsg)
	if !ok {
		t.Fatal("expected terminalRanMsg")
	}
	if msg.err != nil {
		t.Fatalf("run failed: %v", msg.err)
	}
	if !strings.Contains(msg.output, "dmsh-tui-marker") {
		t.Fatalf("output should contain the command result, got %q", msg.output)
	}
	if strings.Contains(msg.output, ">") {
		t.Fatalf("shell prompts leaked into the transcript: %q", msg.output)
	}
	if strings.Contains(msg.output, "\x1b[") {
		t.Fatalf("escape sequences leaked into the transcript: %q", msg.output)
	}

	got := mm.(tuiModel)
	got.printTerminalRun(msg)
	if strings.Contains(got.content, ">") && strings.Contains(got.content, "PS ") {
		t.Fatalf("transcript polluted with shell prompts:\n%s", got.content)
	}
}

func TestResumeFromShellReportsExit(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = modeLabel(config.ModeShell)
	m.rf.cfg.Shell = testShell()
	mm, _ := m.Update(shellExitedMsg{code: 0})
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("state = %d after leaving the shell, want tuiIdle", got.state)
	}
	if !strings.Contains(got.content, "shell exited") {
		t.Fatalf("leaving the shell should be reported:\n%s", got.content)
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
type captureEngine struct {
	tokens []string
	gen    string
}

func (e *captureEngine) Generate(_ context.Context, _, _ string, _ llm.SamplingOptions) (string, error) {
	return e.gen, nil
}

func (e *captureEngine) Stream(_ context.Context, _, _ string, _ llm.SamplingOptions, tokens chan<- string) error {
	for _, t := range e.tokens {
		tokens <- t
	}
	close(tokens)
	return nil
}

func (*captureEngine) Close() error { return nil }

func TestCtrlOOpensModelMenu(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.state != tuiModelMenu {
		t.Fatalf("state = %d, want tuiModelMenu", m.state)
	}
	if len(m.modelItems) == 0 {
		t.Fatal("model menu should list items")
	}
	_, rows := m.layoutRows()
	if len(rows) == 0 {
		t.Fatal("model menu should render rows")
	}
}

func TestModelMenuNavigateUpDown(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	start := m.modelIdx
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.modelIdx != (start+1)%len(m.modelItems) {
		t.Fatalf("modelIdx = %d after down, want %d", m.modelIdx, (start+1)%len(m.modelItems))
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.modelIdx != start {
		t.Fatalf("modelIdx = %d after up, want %d", m.modelIdx, start)
	}
}

func TestModelMenuEscapeCloses(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m.input = "hello"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %d after esc, want tuiIdle", m.state)
	}
	if m.input != "hello" {
		t.Fatalf("input = %q, want it preserved", m.input)
	}
}

func TestModelMenuEnterLoadsInstalled(t *testing.T) {
	m := newTestTui()
	m.state = tuiModelMenu
	m.modelItems = []modelMenuItem{{name: "test-model.gguf", installed: true}}
	m.modelIdx = 0
	mm, cmd := m.menuEnter()
	if cmd == nil {
		t.Fatal("menuEnter should return a pump command while loading")
	}
	loaded := mm.(tuiModel)
	if !loaded.modelBusy {
		t.Fatal("modelBusy should be true while loading")
	}
	if loaded.modelStatus != "loading test-model.gguf" {
		t.Fatalf("modelStatus = %q", loaded.modelStatus)
	}
}

func TestModelMenuTypeKeyClosesAndEdits(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = press(m, keyText("x"))
	if m.state != tuiIdle {
		t.Fatalf("state = %d after typing, want tuiIdle", m.state)
	}
	if m.input != "x" {
		t.Fatalf("input = %q, want %q", m.input, "x")
	}
}

func TestSessionSwitchModelSwapsEngine(t *testing.T) {
	s := &session{
		cfg:    config.Config{ModelPath: "/old.gguf", Threads: 2, CtxSize: 2048, GPULayers: 0},
		engine: &captureEngine{},
	}
	if err := s.switchModel("/new.gguf"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	if s.cfg.ModelPath != "/new.gguf" {
		t.Fatalf("cfg.ModelPath = %q, want /new.gguf", s.cfg.ModelPath)
	}
	if _, ok := s.engine.(*captureEngine); ok {
		t.Fatal("old engine should have been replaced")
	}
}

func TestScrollUpDownWhenInputEmpty(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 50; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	if m.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", m.scrollOffset)
	}
	m.scrollUp()
	if m.scrollOffset != 1 {
		t.Fatalf("scrollUp once: scrollOffset = %d, want 1", m.scrollOffset)
	}
	m.scrollDown()
	if m.scrollOffset != 0 {
		t.Fatalf("scrollDown after scrollUp: scrollOffset = %d, want 0", m.scrollOffset)
	}
	m.scrollDown()
	if m.scrollOffset != 0 {
		t.Fatalf("scrollDown at bottom: scrollOffset = %d, want 0", m.scrollOffset)
	}
	for i := 0; i < 20; i++ {
		m.scrollUp()
	}
	if m.scrollOffset != 20 {
		t.Fatalf("scrollUp 20 times: scrollOffset = %d, want 20", m.scrollOffset)
	}
}

func TestScrollPgUpPgDown(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 100; i++ {
		m.addLine(fmt.Sprintf("line %03d", i))
	}
	// Scroll to near the top first.
	m.scrollOffset = 90
	// PgUp should move up.
	m.scrollBy(-(m.height - 3))
	if m.scrollOffset >= 90 {
		t.Fatalf("scrollBy PgUp: scrollOffset = %d, want < 90", m.scrollOffset)
	}
	// PgDown should move back down.
	m.scrollBy(m.height - 3)
	if m.scrollOffset != 90 {
		t.Fatalf("scrollBy PgDown back: scrollOffset = %d, want 90", m.scrollOffset)
	}
}

func TestArrowsDoNotScrollWhileTyping(t *testing.T) {
	m := newTestTui()
	m.input = "hello"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "hello" {
		t.Fatalf("empty history must leave the input alone, got %q", m.input)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset changed while typing: %d", m.scrollOffset)
	}
}

// Стрелки листают историю всегда, в том числе на пустой строке ввода:
// иначе ↑ на пустом промпте (самый частый случай) не работает вовсе.
func TestArrowsWalkHistoryOnEmptyInput(t *testing.T) {
	m := newTestTui()
	m.history = []string{"first", "second"}
	m.histIdx = len(m.history)

	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "second" {
		t.Fatalf("up on empty input: input = %q, want %q", m.input, "second")
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "first" {
		t.Fatalf("second up: input = %q, want %q", m.input, "first")
	}
	// Выше начала не заворачиваем.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "first" {
		t.Fatalf("up past the start wrapped to %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "" {
		t.Fatalf("down past the end = %q, want empty", m.input)
	}
}

// Стрелки не должны двигать скролл: скролл живёт на PgUp/PgDn.
func TestArrowsDoNotScroll(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 50; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	m.history = []string{"cmd"}
	m.histIdx = 1
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.scrollOffset != 0 {
		t.Fatalf("up must not scroll, scrollOffset = %d", m.scrollOffset)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.scrollOffset != 0 {
		t.Fatalf("down must not scroll, scrollOffset = %d", m.scrollOffset)
	}
}

// PgUp/PgDn скроллят вывод независимо от того, есть ли текст в строке ввода.
func TestPgUpPgDownScrollAlways(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 100; i++ {
		m.addLine(fmt.Sprintf("line %03d", i))
	}
	m.scrollOffset = 90
	m.input = "half-typed"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.scrollOffset >= 90 {
		t.Fatalf("pgup should scroll even with text in the input: %d", m.scrollOffset)
	}
	before := m.scrollOffset
	m = press(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.scrollOffset <= before {
		t.Fatalf("pgdown should scroll forward: %d -> %d", before, m.scrollOffset)
	}
}

// История подтягивается из файла прошлых сессий: без этого стрелки видят
// только текущую сессию.
func TestHistoryLoadsFromFile(t *testing.T) {
	dir := t.TempDir()
	histFile := filepath.Join(dir, "history.jsonl")
	write := func(cmds ...string) {
		var sb strings.Builder
		for _, c := range cmds {
			data, _ := json.Marshal(HistoryEntry{Timestamp: time.Now(), Command: c, Source: "direct"})
			sb.WriteString(string(data) + "\n")
		}
		if err := os.WriteFile(histFile, []byte(sb.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Подряд идущие дубликаты схлопываются. Неподрядные повторы сохраняются:
	// один и тот же Enter дважды подряд — один шаг истории, а та же команда
	// вперемешку с другими — это два разных запуска.
	write("old-cmd", "old-cmd", "older-cmd", "old-cmd")

	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI}}
	s := &session{cfg: config.Config{Mode: config.ModeAI, HistoryFile: histFile}}
	m := NewTuiModel(rf, s).(tuiModel)

	// Порядок файла сохранён как есть; схлопнулась только соседняя пара.
	want := []string{"old-cmd", "older-cmd", "old-cmd"}
	if len(m.history) != len(want) {
		t.Fatalf("history = %v, want %v", m.history, want)
	}
	for i, w := range want {
		if m.history[i] != w {
			t.Fatalf("history = %v, want %v", m.history, want)
		}
	}
	if m.histIdx != len(m.history) {
		t.Fatalf("histIdx = %d, want %d (past the end, ready to walk back)", m.histIdx, len(m.history))
	}
	// Стрелка вверх сразу подставляет последнюю выполненную команду.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "old-cmd" {
		t.Fatalf("up after load: input = %q, want %q", m.input, "old-cmd")
	}
}

// Повтор той же команды не раздувает историю.
func TestRepeatedCommandNotDuplicated(t *testing.T) {
	m := newTestTui()
	m.history = []string{"ls"}
	m.histIdx = 1
	m.input = "ls"
	m.cursorPos = 2
	mm, _ := m.handleEnter()
	got := mm.(tuiModel)
	if len(got.history) != 1 || got.history[0] != "ls" {
		t.Fatalf("history = %v, want a single %q", got.history, "ls")
	}
	m2 := newTestTui()
	m2.input = "pwd"
	m2.cursorPos = 3
	mm2, _ := m2.handleEnter()
	got2 := mm2.(tuiModel)
	if len(got2.history) != 1 || got2.history[0] != "pwd" {
		t.Fatalf("history = %v, want a single %q", got2.history, "pwd")
	}
}

// /history показывает записи из файла вместе с текущей сессией, а не только
// последние 10 выполненных команд.
func TestSlashHistoryShowsFileAndSession(t *testing.T) {
	dir := t.TempDir()
	histFile := filepath.Join(dir, "history.jsonl")
	var sb strings.Builder
	for _, c := range []string{"file-1", "file-2"} {
		data, _ := json.Marshal(HistoryEntry{Timestamp: time.Now(), Command: c, Source: "direct"})
		sb.WriteString(string(data) + "\n")
	}
	if err := os.WriteFile(histFile, []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}

	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI}}
	s := &session{cfg: config.Config{Mode: config.ModeAI, HistoryFile: histFile}}
	m := NewTuiModel(rf, s).(tuiModel)
	s.addHistory("session-1", "direct")

	mm, _ := m.handleSlash("/history")
	got := mm.(tuiModel)
	for _, want := range []string{"file-1", "file-2", "session-1"} {
		if !strings.Contains(got.content, want) {
			t.Fatalf("/history should list %q, content:\n%s", want, got.content)
		}
	}
}

// Модель может сколько угодно возвращать неработающую команду. Без
// ограничения каждая попытка — новый запрос и новый запуск, то есть цикл
// намертво зависает на «thinking…».
func TestAutoCorrectStopsAfterLimit(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	m.s.engine = &captureEngine{}
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "exit 3",
		Risk:    prompt.RiskLow,
	}
	fail := executor.Result{ExitCode: 3, Err: context.DeadlineExceeded}

	attempts := 0
	for {
		attempts++
		if attempts > maxAutoFix+3 {
			t.Fatalf("цикл не остановился: лимит %d, а запросов %d", maxAutoFix, attempts)
		}
		mm, cmd := m.autoCorrect(resp, fail)
		m = mm
		if cmd == nil {
			break
		}
		if m.state != tuiStreaming {
			break
		}
	}

	if attempts != maxAutoFix+1 {
		t.Fatalf("запросов на исправление = %d, want %d (%d попыток + одно сообщение о сдаче)",
			attempts, maxAutoFix+1, maxAutoFix)
	}
	if m.state != tuiIdle {
		t.Fatalf("после сдачи state = %d, want tuiIdle", m.state)
	}
	if !strings.Contains(m.content, "giving up on auto-fix") {
		t.Fatalf("пользователь должен знать, что автокоррекция исчерпана:\n%s", m.content)
	}
}

// Успешная команда возвращает лимит автокоррекции в полное состояние.
func TestAutoCorrectBudgetResetsOnSuccess(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	m.fixTries = 1
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "echo dmsh-auto-fix-marker",
		Risk:    prompt.RiskLow,
	}
	got, _ := m.runCommand(resp)
	if got.fixTries != 0 {
		t.Fatalf("fixTries = %d after a successful command, want 0", got.fixTries)
	}
	if !strings.Contains(got.content, "dmsh-auto-fix-marker") {
		t.Fatalf("command output missing:\n%s", got.content)
	}
}

// Новый запрос пользователя начинает цикл автокоррекции заново.
func TestNewRequestResetsAutoFixBudget(t *testing.T) {
	m := newTestTui()
	m.s.engine = &captureEngine{}
	m.fixTries = maxAutoFix
	m.input = "ls"
	m.cursorPos = 2
	mm, _ := m.handleEnter()
	got := mm.(tuiModel)
	if got.fixTries != 0 {
		t.Fatalf("fixTries = %d after a new request, want 0", got.fixTries)
	}
}

func TestRenderScrollShowsHistory(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 20
	for i := 0; i < 30; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	out := m.render()
	if !strings.Contains(out, "line 29") {
		t.Fatalf("render without scroll should show most recent lines:\n%s", out)
	}
	if strings.Contains(out, "line 05") {
		t.Fatalf("render without scroll should not show oldest lines when content overflows:\n%s", out)
	}
	m.scrollOffset = 10
	out = m.render()
	if strings.Contains(out, "line 29") {
		t.Fatalf("scrolled render should not show newest line when scrolled up:\n%s", out)
	}
	if !strings.Contains(out, "line 05") || !strings.Contains(out, "line 06") {
		t.Fatalf("scrolled render should show older lines:\n%s", out)
	}
}
