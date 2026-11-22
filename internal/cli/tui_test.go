package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

func newTestTui() tuiModel {
	// Mouse включён, как в конфиге по умолчанию: клик по папке в статусной
	// строке — часть интерфейса, а не отдельный режим.
	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI, ModelPath: "/models/q4.gguf", Mouse: true}}
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
	// are scrolled away entirely. Сравниваем по началу строки: последнюю
	// колонку занимает полоса прокрутки.
	newest := "history line"
	for i := 0; i < 4; i++ {
		if !strings.HasPrefix(lines[i], newest) {
			t.Fatalf("scrolled history rows wrong: %v", lines[:4])
		}
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

// Вставка идёт в то поле, которое открыто: у каждого экрана свой буфер, и
// молчаливый переход в режим правки означал бы правку не того поля.
func TestPasteFollowsTheActiveField(t *testing.T) {
	paste := func(m tuiModel) tuiModel {
		t.Helper()
		mm, _ := m.Update(tea.PasteMsg{Content: "текст"})
		return mm.(tuiModel)
	}
	t.Run("prompt", func(t *testing.T) {
		m := paste(newTestTui())
		if m.input != "текст" {
			t.Fatalf("input = %q, want the pasted text", m.input)
		}
	})
	t.Run("palette", func(t *testing.T) {
		m := paste(newTestTui().openPalette())
		if m.input != "текст" {
			t.Fatalf("palette query = %q, want the pasted text", m.input)
		}
	})
	t.Run("model filter", func(t *testing.T) {
		m := newTestTui()
		m.state = tuiModelMenu
		m = paste(m)
		if m.modelFilter != "текст" {
			t.Fatalf("model filter = %q, want the pasted text", m.modelFilter)
		}
	})
	t.Run("history search", func(t *testing.T) {
		m := newTestTui()
		m.history = []string{"ls -la", "текст в истории"}
		mm, _ := m.startSearch()
		m = paste(mm.(tuiModel))
		if m.searchQuery != "текст" {
			t.Fatalf("search query = %q, want the pasted text", m.searchQuery)
		}
		if m.input != "текст в истории" {
			t.Fatalf("search must re-run on the pasted query: %q", m.input)
		}
	})
	t.Run("question", func(t *testing.T) {
		m := newTestTui()
		m.state = tuiQuestion
		m = paste(m)
		if m.input != "текст" {
			t.Fatalf("answer = %q, want the pasted text", m.input)
		}
	})
	t.Run("no text field", func(t *testing.T) {
		m := newTestTui()
		m.state = tuiConfirming
		m = paste(m)
		if m.input != "" {
			t.Fatalf("a confirmation dialog has no field to paste into: %q", m.input)
		}
	})
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
