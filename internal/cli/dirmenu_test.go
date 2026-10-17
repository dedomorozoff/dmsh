package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func click(m tuiModel, x, y int) tuiModel {
	mm, _ := m.handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return mm.(tuiModel)
}

// regionByAction достаёт кликабельную область заданного действия с текущего
// экрана. Тесты кликают именно по ней, поэтому проверка ловит расхождение
// между нарисованным и кликабельным, а не только «клик вообще работает».
func regionByAction(t *testing.T, m tuiModel, act clickAction) clickRegion {
	t.Helper()
	for _, r := range m.clickRegions() {
		if r.act == act {
			return r
		}
	}
	t.Fatalf("no clickable region for action %d on screen:\n%s", act, m.render())
	return clickRegion{}
}

// newDirTestTui готовит окно выбора папки в каталоге с двумя подпапками.
func newDirTestTui(t *testing.T) (tuiModel, string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Chdir(root)

	m := newTestTui()
	m.width, m.height = 80, 24
	return m, root
}

func dirIndexOf(t *testing.T, m tuiModel, name string) int {
	t.Helper()
	for i, it := range m.visibleDirItems() {
		if it.name == name {
			return i
		}
	}
	t.Fatalf("directory %q not found in the list: %+v", name, m.visibleDirItems())
	return -1
}

// Прямоугольник клика должен совпадать с тем, что реально нарисовано: иначе
// клик по папке попал бы в соседний текст статусной строки.
func TestStatusDirRectMatchesRenderedRow(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 12
	m.content = "transcript\n"

	rows := strings.Split(m.render(), "\n")
	last := ansi.Strip(rows[len(rows)-1])
	label := statusDirLabel()
	if !strings.HasPrefix(last, label) {
		t.Fatalf("status row does not start with the folder label:\n%q", last)
	}
	reg := regionByAction(t, m, clickOpenDir)
	if reg.y != len(rows)-1 {
		t.Fatalf("status row = %d, want the last frame row %d", reg.y, len(rows)-1)
	}
	if reg.x0 != 0 || reg.x1 != displayWidth(label) {
		t.Fatalf("columns [%d,%d), want [0,%d) for %q", reg.x0, reg.x1, displayWidth(label), label)
	}
	if act, _ := m.statusActionAt(reg.x0, reg.y); act != clickOpenDir {
		t.Fatalf("action at the folder = %v, want clickOpenDir", act)
	}
}

// Клик по папке открывает выбор каталога; клик рядом — нет.
func TestStatusDirClickOpensMenu(t *testing.T) {
	m, _ := newDirTestTui(t)
	reg := regionByAction(t, m, clickOpenDir)

	got := click(m, reg.x0, reg.y)
	if got.state != tuiDirMenu {
		t.Fatalf("state = %v, want the directory menu", got.state)
	}
	if len(got.dirItems) == 0 {
		t.Fatal("the menu must list the current directory")
	}
	if _, isModal := got.modal(); !isModal {
		t.Fatal("the directory menu must be a modal window")
	}

	// Правее подписи и выше статусной строки клик ничего не делает.
	if after := click(m, reg.x1, reg.y); after.state == tuiDirMenu {
		t.Fatalf("a click past the folder label opened the menu (x=%d)", reg.x1)
	}
	if after := click(m, reg.x0, reg.y-1); after.state == tuiDirMenu {
		t.Fatalf("a click above the status row opened the menu (y=%d)", reg.y-1)
	}
}

// Поверх другого открытого окна клик по статусной строке не должен ничего
// открывать: у палитры свой текст запроса, и перехват молчал бы его.
func TestClickDoesNotStealOtherModal(t *testing.T) {
	m, _ := newDirTestTui(t)
	m = m.openPalette()
	m = press(m, keyText("cd"))
	if got := click(m, 0, m.height-1); got.state != tuiPalette {
		t.Fatalf("state = %v, want the palette to stay open", got.state)
	}
}

func TestMouseDisabledIgnoresClicks(t *testing.T) {
	m, _ := newDirTestTui(t)
	m.rf.cfg.Mouse = false
	if m.mouseEnabled() {
		t.Fatal("mouseEnabled must follow the config")
	}
	reg := regionByAction(t, m, clickOpenDir)
	if got := click(m, reg.x0, reg.y); got.state == tuiDirMenu {
		t.Fatal("with mouse=false a click must not open the menu")
	}
	// Выключенная мышь не должна и включаться в терминале.
	if m.renderView().MouseMode != tea.MouseModeNone {
		t.Fatal("view must not request mouse events when mouse=false")
	}
}

func TestViewRequestsMouseOutsideTerminal(t *testing.T) {
	m, _ := newDirTestTui(t)
	if m.renderView().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the TUI must ask for mouse clicks: the folder in the status line is a button")
	}
	m.state = tuiTerminal
	if got := m.renderView().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("mouse mode in terminal mode = %v, want none: the mouse belongs to the shell", got)
	}
}

// В терминале мышь не должна перехватываться: там кадр терминала, а клик по
// строке состояния ушёл бы мимо.
func TestTerminalStateIgnoresMouse(t *testing.T) {
	m, _ := newDirTestTui(t)
	m.state = tuiTerminal
	if got := click(m, 0, m.height-1); got.state == tuiDirMenu {
		t.Fatal("a click in terminal mode must not open the menu")
	}
}

// Enter открывает выделенную папку и обновляет список под новый каталог.
func TestDirMenuEnterChangesDirectory(t *testing.T) {
	m, root := newDirTestTui(t)
	m = m.openDirMenu()

	m.dirIdx = dirIndexOf(t, m, "alpha")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if wd != filepath.Join(root, "alpha") {
		t.Fatalf("cwd = %q, want the alpha subdirectory", wd)
	}
	if m.state != tuiDirMenu {
		t.Fatalf("state = %v, want the menu to stay open for a deeper dive", m.state)
	}
	if m.dirErr != "" {
		t.Fatalf("dirErr = %q after a successful cd", m.dirErr)
	}
	if !strings.Contains(m.dirMenuModal().title, "alpha") {
		t.Fatalf("the modal title must show the new directory: %q", m.dirMenuModal().title)
	}
}

// Клик по строке окна делает то же, что Enter: это тот же список.
func TestDirMenuClickOpensFolder(t *testing.T) {
	m, root := newDirTestTui(t)
	m = m.openDirMenu()

	idx := dirIndexOf(t, m, "beta")
	x, y := m.dirMenuRowPoint(idx)
	m = click(m, x, y)

	wd, _ := os.Getwd()
	if wd != filepath.Join(root, "beta") {
		t.Fatalf("cwd = %q, want the beta subdirectory", wd)
	}
}

// dirMenuRowPoint — координаты клика по строке пункта в окне. Раскладка
// повторяет overlayModal: рамка сверху и слева съедает по строке и
// колонке. Расхождение с ней означало бы, что клик попадает на соседний
// пункт, поэтому координаты проверяются попутно, попаданием в тот же пункт.
func (m tuiModel) dirMenuRowPoint(idx int) (x, y int) {
	l := m.dirMenuLayout()
	lines, _, _ := frameBoxLines(m.dirMenuModal(), m.width)
	boxW := displayWidth(lines[0])
	originX := max(0, (m.width-boxW)/2)
	originY := max(0, (m.height-len(lines))/2)
	return originX + 2, originY + 1 + l.firstRowIdx + idx - l.start
}

func TestDirMenuHitMatchesRows(t *testing.T) {
	m, _ := newDirTestTui(t)
	m = m.openDirMenu()
	for i, it := range m.visibleDirItems() {
		x, y := m.dirMenuRowPoint(i)
		if got := m.dirMenuHit(x, y); got != i {
			t.Fatalf("dirMenuHit(%d,%d) = %d, want %d (%s)", x, y, got, i, it.name)
		}
	}
	// Клик выше окна и ниже его содержимого ничего не выбирает.
	if got := m.dirMenuHit(0, 0); got != -1 {
		t.Fatalf("a click outside the window = %d, want -1", got)
	}
}

// ".." идёт наверх, а в корне родителя нет — там список не должен предлагать
// переход, который ничего не делает.
func TestDirMenuParentGoesUp(t *testing.T) {
	m, root := newDirTestTui(t)
	parent := filepath.Dir(root)
	m = m.openDirMenu()

	m.dirIdx = dirIndexOf(t, m, "..")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	wd, _ := os.Getwd()
	if wd != parent {
		t.Fatalf("cwd = %q, want the parent %q", wd, parent)
	}
	parentMenu := m.openDirMenu()
	for _, it := range parentMenu.visibleDirItems() {
		if it.name == ".." && it.path == wd {
			t.Fatalf("the root directory must not offer a parent: %+v", it)
		}
	}
}

func TestDirMenuListsDirectoriesOnly(t *testing.T) {
	m, _ := newDirTestTui(t)
	m = m.openDirMenu()
	for _, it := range m.visibleDirItems() {
		if it.name == "readme.md" {
			t.Fatalf("files must not be listed: %+v", it)
		}
	}
}

// Фильтр сужает список, но родитель и домашняя папка остаются: иначе
// нечем выйти из фильтра, под который не подошёл ни один каталог.
func TestDirMenuFilterKeepsShortcuts(t *testing.T) {
	m, _ := newDirTestTui(t)
	m = m.openDirMenu()

	m = press(m, keyText("a"))
	m = press(m, keyText("l"))
	m = press(m, keyText("p"))
	if m.dirFilter != "alp" {
		t.Fatalf("filter = %q, want %q", m.dirFilter, "alp")
	}
	items := m.visibleDirItems()
	if len(items) != 3 {
		t.Fatalf("filtered list = %+v, want .., ~ and alpha", items)
	}
	if items[len(items)-1].name != "alpha" {
		t.Fatalf("filtered list = %+v, want alpha to match", items)
	}

	// Esc сначала снимает фильтр, и только потом закрывает окно.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiDirMenu || m.dirFilter != "" {
		t.Fatalf("Esc must clear the filter first: state=%v filter=%q", m.state, m.dirFilter)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %v, want idle after closing", m.state)
	}
}

// Модальное окно обязано перехватывать клавиши: F1 и Ctrl+Q работают и здесь,
// иначе из окна папок нельзя было бы выйти.
func TestDirMenuHandlesKeys(t *testing.T) {
	m, _ := newDirTestTui(t)
	m = m.openDirMenu()
	m.dirIdx = dirIndexOf(t, m, "beta")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.visibleDirItems()[m.dirIdx].name; got != "alpha" {
		t.Fatalf("after Up the selection is %q, want alpha", got)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.visibleDirItems()[m.dirIdx].name; got != "beta" {
		t.Fatalf("after Down the selection is %q, want beta", got)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyF1})
	if !strings.Contains(m.content, "dmsh help") {
		t.Fatal("F1 must show help from the directory menu")
	}
	m2 := m.openDirMenu()
	if _, cmd := m2.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("Ctrl+Q must quit from the directory menu")
	}
}

// Колесо листает скроллбек, а в окне папок — список.
func TestMouseWheelScrolls(t *testing.T) {
	m, _ := newDirTestTui(t)
	m.content = strings.Repeat("line\n", 50)
	m.scrollOffset = 10

	mm, _ := m.handleMouseWheel(tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown})
	if got := mm.(tuiModel).scrollOffset; got != 13 {
		t.Fatalf("scrollOffset after wheel down = %d, want 13", got)
	}
	mm, _ = m.handleMouseWheel(tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelUp})
	if got := mm.(tuiModel).scrollOffset; got != 7 {
		t.Fatalf("scrollOffset after wheel up = %d, want 7", got)
	}

	m = m.openDirMenu()
	mm, _ = m.handleMouseWheel(tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown})
	if got := mm.(tuiModel).dirIdx; got == 0 {
		t.Fatal("the wheel must move the selection in the directory menu")
	}
}
