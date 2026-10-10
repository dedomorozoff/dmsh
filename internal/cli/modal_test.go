package cli

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
)

// Рамка модального окна должна быть одной ширины во всех строках и не
// выходить за терминал: иначе окно «едет» и кадр разъезжается по колонкам.
// modalCases — все модальные окна TUI. Проверки раскладки и глобальных
// клавиш идут по этому списку, чтобы новое окно нельзя было забыть.
func modalCases() []struct {
	name string
	open func(tuiModel) tuiModel
} {
	return []struct {
		name string
		open func(tuiModel) tuiModel
	}{
		{"palette", func(m tuiModel) tuiModel { return m.openPalette() }},
		{"settings", func(m tuiModel) tuiModel { return m.openSettings() }},
		{"setup", func(m tuiModel) tuiModel { return m.openSetup() }},
		{"mode", func(m tuiModel) tuiModel { return m.openModeMenu() }},
	}
}

func TestModalBoxRowsShareWidth(t *testing.T) {
	for _, width := range []int{20, 30, 40, 60, 120} {
		for _, tc := range modalCases() {
			m := tc.open(newTestTui())
			m.width, m.height = width, 30
			m.content = "transcript line\n"
			box, _ := m.modal()
			rendered, _, _ := frameBoxLines(box, width)
			want := displayWidth(rendered[0])
			for i, row := range rendered {
				if got := displayWidth(row); got != want {
					t.Fatalf("%s at width=%d: line %d is %d wide, want %d", tc.name, width, i, got, want)
				}
			}
			if want > width {
				t.Fatalf("%s at width=%d: box is %d wide, want <= %d", tc.name, width, want, width)
			}
		}
	}
}

// С открытым окном кадр должен остаться прежнего размера: ровно height
// строк, каждая не шире терминала. Иначе буфер кадра поедет и терминал
// начнёт печатать мусор внизу экрана.
func TestModalKeepsFrameSize(t *testing.T) {
	for _, tc := range modalCases() {
		for _, width := range []int{24, 40, 100} {
			m := tc.open(newTestTui())
			m.width, m.height = width, 24
			m.addLine("line one")
			m.addLine("line two")
			rows := strings.Split(m.render(), "\n")
			if len(rows) != m.height {
				t.Fatalf("%s at width=%d: frame has %d rows, want %d", tc.name, width, len(rows), m.height)
			}
			for i, row := range rows {
				if got := displayWidth(row); got > width {
					t.Fatalf("%s at width=%d: row %d is %d wide:\n%s", tc.name, width, i, got, m.render())
				}
			}
		}
	}
}

// Курсор модального окна должен попадать внутрь рамки: иначе терминал
// показывает его в другом месте экрана, и правка выглядит сломанной.
func TestModalCursorInsideBox(t *testing.T) {
	m := newSettingsTui()
	m.width, m.height = 80, 24
	m.setIdx = setRowHost
	frame, col, y := overlayModal(m.renderFrame(), m.settingsModal(), m.width, m.height)
	lines, _, _ := frameBoxLines(m.settingsModal(), m.width)
	top := -1
	bottom := -1
	for i, row := range frame {
		if strings.Contains(row, lines[0]) {
			top = i
			break
		}
	}
	for i := len(frame) - 1; i >= 0; i-- {
		if strings.Contains(frame[i], lines[len(lines)-1]) {
			bottom = i
			break
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("box borders not found in frame:\n%s", strings.Join(frame, "\n"))
	}
	if y < top || y > bottom {
		t.Fatalf("cursor row %d is outside the box rows %d..%d", y, top, bottom)
	}
	if col < 0 || col >= m.width {
		t.Fatalf("cursor column %d is outside the terminal width %d", col, m.width)
	}
}

// Палитра показывает запрос в рамке, но сам текст продолжает жить в
// строке ввода: закрытие окна возвращает то, что было до Ctrl+P.
func TestPaletteModalKeepsInputInModel(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 80, 24
	m.input = "черновик"
	m.cursorPos = runeSliceLen(m.input)
	m = press(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = press(m, tea.KeyPressMsg{Text: "help", Code: 'h'})
	box := m.paletteModal()
	if !strings.Contains(box.rows[1], "help") {
		t.Fatalf("palette query row should show the typed text: %q", box.rows[1])
	}
	if box.cursorY != 1 {
		t.Fatalf("palette cursor row = %d, want the query row", box.cursorY)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.input != "черновик" {
		t.Fatalf("input after Esc = %q, want the pre-palette text", m.input)
	}
}

// F1 и Ctrl+Q работают поверх окна — иначе из палитры нельзя было бы
// выйти, не закрыв её.
func TestModalStillHandlesGlobalKeys(t *testing.T) {
	for _, tc := range modalCases() {
		m := tc.open(newTestTui())
		m = press(m, tea.KeyPressMsg{Code: tea.KeyF1})
		if !strings.Contains(m.content, "dmsh help") {
			t.Fatalf("%s: F1 must show help from the modal", tc.name)
		}
		m2 := tc.open(newTestTui())
		_, cmd := m2.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatalf("%s: Ctrl+Q must quit from the modal", tc.name)
		}
	}
}
