package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// clickRegionFor находит кликабельную область действия с номером arg.
func clickRegionFor(t *testing.T, m tuiModel, act clickAction, arg int) clickRegion {
	t.Helper()
	for _, r := range m.clickRegions() {
		if r.act == act && r.arg == arg {
			return r
		}
	}
	t.Fatalf("no clickable region for action %d (arg %d):\n%s", act, arg, m.render())
	return clickRegion{}
}

// clickAct кликает по области левым краем: любая её колонка ведёт туда же.
func clickAct(t *testing.T, m tuiModel, act clickAction, arg int) tuiModel {
	t.Helper()
	r := clickRegionFor(t, m, act, arg)
	return click(m, r.x0, r.y)
}

// Каждая кнопка статусной строки открывает то, что подписана: иначе подпись
// врёт, и это обнаруживается только при использовании.
func TestStatusButtonsOpenTheirWindows(t *testing.T) {
	newTui := func() tuiModel {
		m := newTestTui()
		m.width, m.height = 120, 24
		return m
	}

	t.Run("dir", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickOpenDir, 0); got.state != tuiDirMenu {
			t.Errorf("state = %v, want tuiDirMenu", got.state)
		}
	})
	t.Run("mode", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickOpenModeMenu, 0); got.state != tuiModeMenu {
			t.Errorf("state = %v, want tuiModeMenu", got.state)
		}
	})
	t.Run("model", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickOpenModelMenu, 0); got.state != tuiModelMenu {
			t.Errorf("state = %v, want tuiModelMenu", got.state)
		}
	})
	t.Run("proxy", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickOpenSettings, 0); got.state != tuiSettings {
			t.Errorf("state = %v, want tuiSettings", got.state)
		}
	})
	t.Run("palette", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickOpenPalette, 0); got.state != tuiPalette {
			t.Errorf("state = %v, want tuiPalette", got.state)
		}
	})
	t.Run("help", func(t *testing.T) {
		if got := clickAct(t, newTui(), clickShowHelp, 0); !strings.Contains(got.content, "dmsh help") {
			t.Error("the help hint must show help")
		}
	})
	t.Run("cycle", func(t *testing.T) {
		m := newTui()
		got := clickAct(t, m, clickCycleMode, 0)
		if got.modeLabel == m.modeLabel {
			t.Errorf("the mode hint must switch the mode (still %q)", got.modeLabel)
		}
	})
}

// frameRow — строка кадра без ANSI: клик должен попадать в ту строку, которую
// нарисовали, а не в соседнюю.
func frameRow(t *testing.T, m tuiModel, y int) string {
	t.Helper()
	frame := strings.Split(m.render(), "\n")
	if y < 0 || y >= len(frame) {
		t.Fatalf("row %d is outside the frame (%d rows)", y, len(frame))
	}
	return ansi.Strip(frame[y])
}

// Кнопка должна закрывать свой текст на экране: иначе клик уедет на соседнее
// слово, и это видно только в живом терминале.
func TestStatusButtonsMatchRenderedText(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 120, 24
	m.content = "transcript\n"
	want := map[clickAction]string{
		clickOpenDir:       statusDirLabel(),
		clickOpenModeMenu:  "mode:" + m.modeLabel,
		clickOpenModelMenu: "model:" + m.modelLabel(),
		clickOpenSettings:  m.proxyLabel(),
		clickShowHelp:      "help",
		clickOpenPalette:   "palette",
		clickCycleMode:     "mode",
	}
	for act, text := range want {
		r := clickRegionFor(t, m, act, 0)
		if r.y != m.height-1 {
			t.Errorf("action %d is on row %d, want the status row %d", act, r.y, m.height-1)
			continue
		}
		row := frameRow(t, m, r.y)
		if r.x1 > len(row) {
			t.Errorf("action %d reaches past the row: %+v", act, r)
			continue
		}
		if got := strings.TrimSpace(row[r.x0:r.x1]); got != text {
			t.Errorf("action %d covers %q, want exactly %q", act, got, text)
		}
	}
}

// Клик по строке окна делает то же, что Enter на ней: одно правило на все
// окна, иначе мышь и клавиатура разъедутся.
func TestModalRowClickEqualsEnter(t *testing.T) {
	m := newSettingsTui()
	m.width, m.height = 100, 30
	got := clickAct(t, m, clickSettingRow, setRowMode)
	if got.setMode == m.setMode {
		t.Errorf("clicking the proxy mode row did not change it (still %q)", got.setMode)
	}

	// Строка значения: клик открывает правку, Enter на ней — тоже.
	m = newSettingsTui()
	m.width, m.height = 100, 30
	got = clickAct(t, m, clickSettingRow, setRowHost)
	if !got.setEditing {
		t.Error("clicking a value row must start editing it")
	}
	if got.setIdx != setRowHost {
		t.Errorf("setIdx = %d, want %d", got.setIdx, setRowHost)
	}
}

// У каждого окна должны быть кликабельные строки, и ни одна область не должна
// выходить за кадр.
func TestModalRowsClickable(t *testing.T) {
	for _, tc := range modalCases() {
		m := tc.open(newTestTui())
		m.width, m.height = 100, 30
		regions := m.clickRegions()
		// Окно задач бывает пустым, и кликать в нём нечего — это отдельный
		// случай (TestTodoWindowOnEmptyList).
		if tc.name == "todo" {
			continue
		}
		if len(regions) == 0 {
			t.Errorf("%s: no clickable rows", tc.name)
		}
		if len(regions) > len(m.modalBox().rows) {
			t.Errorf("%s: %d regions for %d rows", tc.name, len(regions), len(m.modalBox().rows))
		}
		for _, r := range regions {
			if r.x0 < 0 || r.x1 > m.width || r.y < 0 || r.y >= m.height {
				t.Errorf("%s: region %+v is outside the frame %dx%d", tc.name, r, m.width, m.height)
			}
		}
	}
}

// Пункт списка режимов кликается и применяется так же, как Enter.
func TestModeMenuRowClickApplies(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 30
	m = m.openModeMenu()
	got := clickAct(t, m, clickModeItem, 2) // terminal
	if got.state != tuiIdle {
		t.Fatalf("state = %v, want the menu to close after a click", got.state)
	}
	if got.rf.cfg.Mode != config.ModeShell {
		t.Errorf("mode = %q, want %q", got.rf.cfg.Mode, config.ModeShell)
	}
}

// Пункт палитры кликом выполняется, как Enter: два клика на команду — это
// ровно то, от чего палитра избавляет.
func TestPaletteItemClickRunsCommand(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 30
	m = m.openPalette()

	idx := -1
	for i, it := range paletteMatches("") {
		if it.name == "/help" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("/help must be in the palette")
	}
	r := clickRegionFor(t, m, clickPaletteItem, idx)
	if row := frameRow(t, m, r.y); !strings.Contains(row, "/help") {
		t.Fatalf("the clickable row does not show /help:\n%s", row)
	}

	got := click(m, r.x0, r.y)
	if got.state == tuiPalette {
		t.Fatal("clicking a palette item must run it and close the palette")
	}
	if !strings.Contains(got.content, "dmsh help") {
		t.Errorf("/help must show the help:\n%s", got.content)
	}
}

// Пункт меню моделей кликается и выбирается, как Enter на строке.
func TestModelMenuItemClickSelects(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 30
	m, _ = m.openModelMenu()
	items := m.visibleModelItems()
	if len(items) < 2 {
		t.Skip("the local catalog is empty in this environment")
	}
	// Клик должен попадать в строку именно этой модели: иначе выбор уедет
	// на соседний пункт каталога.
	r := clickRegionFor(t, m, clickModelItem, 1)
	if row := frameRow(t, m, r.y); !strings.Contains(row, items[1].name) {
		t.Fatalf("the clickable row does not show %q:\n%s", items[1].name, row)
	}
	got := click(m, r.x0, r.y)
	if got.state == tuiModelMenu && got.modelIdx != 1 {
		t.Errorf("modelIdx = %d, want the clicked item 1", got.modelIdx)
	}
}

// Во время правки поля клик не должен ничего подтверждать: Enter в поле ввода
// завершает правку, и потерять напечатанное из-за клика обидно.
func TestEditingRowsAreNotClickable(t *testing.T) {
	m := newSettingsTui()
	m.width, m.height = 100, 30
	m.setEditing = true
	for _, r := range m.clickRegions() {
		if r.act == clickSettingRow {
			t.Fatalf("the editing screen must not have clickable rows: %+v", r)
		}
	}
	if got := click(m, 0, 0); got.state != tuiSettings {
		t.Fatalf("a click on the editing screen changed the state to %v", got.state)
	}
}

// Промах мимо окна ничего не делает: закрывать окно вслепую нельзя, в
// настройках это потеряло бы несохранённые значения.
func TestClickOutsideModalDoesNothing(t *testing.T) {
	m := newSettingsTui()
	m.width, m.height = 100, 30
	m = m.openSettings()
	for _, pt := range [][2]int{{0, 0}, {m.width - 1, 0}, {0, m.height - 1}} {
		if got := click(m, pt[0], pt[1]); got.state != tuiSettings {
			t.Fatalf("a click at %v closed the settings", pt)
		}
	}
}

// Клик по терминалу не перехватывается: там кадр оболочки.
func TestTerminalStateHasNoRegions(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 24
	m.state = tuiTerminal
	if got := m.clickRegions(); len(got) != 0 {
		t.Fatalf("terminal mode must expose no clickable regions: %+v", got)
	}
}

// Курсор и клик должны указывать на одну и ту же строку выбранной модели:
// иначе при фильтре или длинном каталоге выделение и клик разъедутся.
func TestModelMenuCursorFollowsSelection(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 24
	m, _ = m.openModelMenu()
	// Фильтр добавляет строку над списком и сдвигает его на строку вниз.
	m.modelFilter = "instruct"
	visible := m.visibleModelItems()
	if len(visible) < 2 {
		t.Skip("the local catalog does not match the filter twice")
	}
	m.modelIdx = 1
	r := clickRegionFor(t, m, clickModelItem, 1)
	if row := frameRow(t, m, r.y); !strings.Contains(row, visible[1].name) {
		t.Fatalf("the clickable row does not show the selected model:\n%s", row)
	}
	if v := m.renderView(); v.Cursor == nil || v.Cursor.Position.Y != r.y {
		got := -1
		if v.Cursor != nil {
			got = v.Cursor.Position.Y
		}
		t.Errorf("cursor row = %d, want the selected row %d", got, r.y)
	}
}

// Ни одна область не должна выходить за кадр ни на одном экране: клик по
// обрезанному краю попал бы не туда, куда нарисовано.
func TestClickRegionsStayInsideFrame(t *testing.T) {
	screens := map[string]func(tuiModel) tuiModel{
		"idle": func(m tuiModel) tuiModel { return m },
		"model menu": func(m tuiModel) tuiModel {
			mm, _ := m.openModelMenu()
			return mm
		},
	}
	for _, tc := range modalCases() {
		screens[tc.name] = tc.open
	}
	for name, open := range screens {
		m := open(newTestTui())
		m.width, m.height = 90, 20
		m.content = strings.Repeat("line\n", 5)
		for _, r := range m.clickRegions() {
			if r.x0 < 0 || r.x1 > m.width || r.y < 0 || r.y >= m.height {
				t.Errorf("%s: region %+v is outside the frame %dx%d", name, r, m.width, m.height)
			}
			if r.act == clickNone {
				t.Errorf("%s: region without an action %+v", name, r)
			}
		}
		// Каждая область должна отвечать за свою точку.
		for _, r := range m.clickRegions() {
			act, arg := m.clickActionAt(r.x0, r.y)
			if act != r.act || arg != r.arg {
				t.Errorf("%s: click at %d,%d gives (%d,%d), want (%d,%d)", name, r.x0, r.y, act, arg, r.act, r.arg)
			}
		}
	}
}

// Статусная строка не должна разъезжаться: подписи на месте при любой
// ширине, и она не выходит за терминал.
func TestStatusLineKeepsLabels(t *testing.T) {
	m := newTestTui()
	m.height = 24
	for _, w := range []int{60, 80, 120, 200} {
		m.width = w
		line := ansi.Strip(m.statusLine())
		for _, want := range []string{statusDirLabel(), "mode:", "model:", m.proxyLabel()} {
			if !strings.Contains(line, want) {
				t.Errorf("width=%d: status line lost %q:\n%s", w, want, line)
			}
		}
		if got := displayWidth(m.statusline()); got > w {
			t.Errorf("width=%d: status line is %d wide", w, got)
		}
	}
}
