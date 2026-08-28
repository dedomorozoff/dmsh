package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
)

func newTestTui() tuiModel {
	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI, ModelPath: "/models/q4.gguf"}}
	return NewTuiModel(rf, &session{cfg: rf.cfg}).(tuiModel)
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
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.input != "/clear" {
		t.Fatalf("ctrl+p should step up in the menu, input = %q", m.input)
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

func TestCtrlPOnEmptyHistoryGivesFeedback(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !strings.Contains(m.content, "history is empty") {
		t.Fatalf("ctrl+p with no history should show feedback:\n%s", m.content)
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

func TestShellModeNoLLM(t *testing.T) {
	m := newTestTui()
	m.rf.cfg.Mode = config.ModeShell
	m.modeLabel = string(config.ModeShell)
	m.input = "pwd date"
	m.cursorPos = 8
	mm, cmd := m.handleEnter()
	if cmd != nil {
		t.Fatal("shell mode must not submit to the LLM")
	}
	m = mm.(tuiModel)
	if m.streaming || m.state == tuiStreaming {
		t.Fatal("shell mode must not enter streaming/LLM state")
	}
	if !strings.Contains(m.content, "$ pwd date") {
		t.Fatalf("shell mode should echo the executed command, content:\n%s", m.content)
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

func TestScrollKeyWhenInputHasTextFallsBackToHistory(t *testing.T) {
	m := newTestTui()
	m.input = "hello"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "hello" {
		t.Fatalf("input changed while typing: %q", m.input)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset changed while typing: %d", m.scrollOffset)
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
