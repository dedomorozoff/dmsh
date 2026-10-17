package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/vt"
)

// dirItem — строка окна выбора папки: путь и подпись. Подписи ".." и "~" —
// это ярлыки родителя и домашней папки, они не совпадают с именем папки.
type dirItem struct {
	path string
	name string
}

// dirMaxVisible — сколько папок помещается в окно. Список едет за выделением
// через scrollWindow: длинный каталог не должен превращаться в первые
// десять строк с подсказкой «(+N more)».
const dirMaxVisible = 10

// listDirItems собирает список для текущего каталога: родитель, домашняя
// папка и подкаталоги. Файлы не показываем — окно меняет рабочий каталог, а
// не открывает файлы.
func listDirItems() ([]dirItem, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		return nil, err
	}
	items := make([]dirItem, 0, len(entries)+2)
	if parent := filepath.Dir(wd); parent != wd {
		items = append(items, dirItem{path: parent, name: ".."})
	}
	if home, err := os.UserHomeDir(); err == nil && home != wd && home != "" {
		items = append(items, dirItem{path: home, name: "~"})
	}
	for _, e := range entries {
		if !dirEntryIsDir(wd, e.Name(), e) {
			continue
		}
		items = append(items, dirItem{path: filepath.Join(wd, e.Name()), name: e.Name()})
	}
	return items, nil
}

// dirEntryIsDir отличает папку от файла. os.DirEntry описывает сам симлинк,
// поэтому линк на папку виден папкой только после Stat по цели.
func dirEntryIsDir(dir, name string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.IsDir()
}

// reloadDirs перечитывает список папок текущего каталога. Ошибка чтения
// показывается в окне, а не теряется: недоступный каталог — это ответ, а не
// повод закрыть окно, из которого в него и пришли.
func (m *tuiModel) reloadDirs() {
	m.dirItems, m.dirErr = nil, ""
	items, err := listDirItems()
	if err != nil {
		m.dirErr = err.Error()
		return
	}
	m.dirItems = items
}

// openDirMenu открывает окно выбора папки.
func (m tuiModel) openDirMenu() tuiModel {
	m.state = tuiDirMenu
	m.dirIdx = 0
	m.dirFilter = ""
	m.reloadDirs()
	return m
}

// closeDirMenu возвращает обычный экран. Рабочий каталог при этом остаётся
// тем, куда ушли: close ничего не откатывает.
func (m tuiModel) closeDirMenu() tuiModel {
	m.state = tuiIdle
	m.dirItems = nil
	m.dirFilter = ""
	m.dirErr = ""
	m.dirIdx = 0
	return m
}

// visibleDirItems — список после фильтра. Ярлыки родителя и домашней папки
// остаются всегда: иначе фильтр, под который не подошёл ни один каталог,
// оставил бы пользователя вообще без возможности выйти наверх.
func (m tuiModel) visibleDirItems() []dirItem {
	f := strings.ToLower(strings.TrimSpace(m.dirFilter))
	if f == "" {
		return m.dirItems
	}
	out := make([]dirItem, 0, len(m.dirItems))
	for _, it := range m.dirItems {
		if isDirShortcut(it.name) || strings.Contains(strings.ToLower(it.name), f) {
			out = append(out, it)
		}
	}
	return out
}

func isDirShortcut(name string) bool { return name == ".." || name == "~" }

// dirMenuLayout — строки окна вместе с раскладкой: полуинтервал показанных
// пунктов и номер строки первого из них. И мышь, и отрисовка считают по этим
// числам, иначе клик попадал бы на соседний пункт.
type dirMenuLayout struct {
	rows        []string
	start, end  int
	firstRowIdx int
}

func (m tuiModel) dirMenuLayout() dirMenuLayout {
	items := m.visibleDirItems()
	start, end := scrollWindow(m.dirIdx, len(items), dirMaxVisible)

	rows := make([]string, 0, end-start+4)
	hint := "click or Enter open · ↑/↓ pick · type to filter · Esc close"
	if len(items) > dirMaxVisible {
		hint = fmt.Sprintf("%s (%d–%d of %d)", hint, start+1, end, len(items))
	}
	rows = append(rows, fitWidth(colorGray+hint+colorReset, m.width))
	if m.dirFilter != "" {
		rows = append(rows, fitWidth(fmt.Sprintf("%sfilter:%s %s", colorGray, colorGreen, m.dirFilter)+colorReset, m.width))
	}
	switch {
	case m.dirErr != "":
		rows = append(rows, fitWidth(colorRed+m.dirErr+colorReset, m.width))
	case len(items) == 0:
		rows = append(rows, fitWidth(colorGray+"no subdirectories"+colorReset, m.width))
	}
	first := len(rows)
	for i := start; i < end; i++ {
		marker, style := "  ", colorGray
		if i == m.dirIdx {
			marker, style = "> ", colorYellow+colorBold
		}
		note := ""
		switch items[i].name {
		case "..":
			note = "  (parent)"
		case "~":
			note = "  (home)"
		}
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s%s%s%s%s",
			style, marker, items[i].name, colorReset, colorGray, note, colorReset), m.width))
	}
	return dirMenuLayout{rows: rows, start: start, end: end, firstRowIdx: first}
}

// dirMenuModal описывает окно выбора папки для рендера. Заголовок показывает
// текущий каталог: переходы идут вглубь, поэтому видно, где мы находимся.
func (m tuiModel) dirMenuModal() modalBox {
	l := m.dirMenuLayout()
	cursorY := l.firstRowIdx
	if l.end > l.start {
		cursorY = l.firstRowIdx + m.dirIdx - l.start
	}
	if cursorY >= len(l.rows) {
		cursorY = len(l.rows) - 1
	}
	wd, _ := os.Getwd()
	return modalBox{title: "cd: " + shortPath(wd), rows: l.rows, cursorX: 0, cursorY: max(0, cursorY)}
}

// dirMenuHit возвращает индекс пункта под курсором мыши или -1, если клик
// промахнулся. Попадание считает общий для окон код (modalRowAt +
// modalRowAction), иначе клик вёл бы в соседнее окно.
func (m tuiModel) dirMenuHit(x, y int) int {
	for _, r := range m.clickRegions() {
		if r.act != clickDirItem || r.y != y || x < r.x0 || x >= r.x1 {
			continue
		}
		return r.arg
	}
	return -1
}

// handleDirMenuKey обрабатывает клавиши в окне выбора папки. Печатные символы
// фильтруют список, а не закрывают окно: в каталоге бывает много папок, и
// искать их стрелками нечем.
func (m tuiModel) handleDirMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.visibleDirItems()
	switch {
	case msg.Code == tea.KeyEscape || msg.Keystroke() == "ctrl+c":
		// Esc сначала снимает фильтр: закрывать окно, потеряв список,
		// неудобно.
		if m.dirFilter != "" {
			m.dirFilter = ""
			m.dirIdx = 0
			return m, nil
		}
		return m.closeDirMenu(), nil
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.dirMove(-1, len(items))
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.dirMove(1, len(items))
		return m, nil
	case msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h":
		if r := []rune(m.dirFilter); len(r) > 0 {
			m.dirFilter = string(r[:len(r)-1])
			m.dirIdx = 0
		}
		return m, nil
	case msg.Code == tea.KeyEnter:
		return m.dirMenuEnter()
	}
	if isInputKey(msg) {
		m.dirFilter += msg.Text
		m.dirIdx = 0
	}
	return m, nil
}

// dirMove сдвигает выделение по списку из n пунктов и заворачивает на краях:
// в окне папок переход «через край» привычнее, чем остановка.
func (m *tuiModel) dirMove(dir, n int) {
	if n <= 0 {
		m.dirIdx = 0
		return
	}
	m.dirIdx = ((m.dirIdx+dir)%n + n) % n
}

// dirMenuEnter открывает выделенную папку.
func (m tuiModel) dirMenuEnter() (tea.Model, tea.Cmd) {
	items := m.visibleDirItems()
	if len(items) == 0 {
		return m, nil
	}
	if m.dirIdx >= len(items) {
		m.dirIdx = len(items) - 1
	}
	return m.changeDir(items[m.dirIdx].path)
}

// changeDir меняет рабочий каталог и перечитывает список. Окно остаётся
// открытым: переходы идут вглубь, и закрывать его на каждом шаге было бы
// лишним кликом. Ошибка остаётся на экране — с неё выбраться можно, просто
// уйти наверх через "..".
func (m tuiModel) changeDir(path string) (tea.Model, tea.Cmd) {
	if err := os.Chdir(path); err != nil {
		m.dirErr = err.Error()
		return m, nil
	}
	m.dirErr = ""
	m.dirFilter = ""
	m.dirIdx = 0
	m.reloadDirs()
	return m, nil
}

// mouseEnabled — включена ли мышь в TUI. Пока она включена, терминал
// отправляет события мыши приложению, а не выделяет текст сам, поэтому
// выключается флагом --mouse=false или `dmsh config set mouse false`.
func (m tuiModel) mouseEnabled() bool {
	return m.rf != nil && m.rf.cfg.Mouse
}

// handleMouseClick отвечает на клик: находит область кадра и выполняет её
// действие. В режиме терминала мышь принадлежит оболочке внутри окна.
func (m tuiModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if !m.mouseEnabled() || msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.state == tuiTerminal {
		if m.inTerminalScreen(msg.X, msg.Y) {
			m.forwardMouse(m.mouseEvent(vt.MousePress, vt.MouseLeft, msg.Mod, msg.X, msg.Y))
		}
		return m, nil
	}
	act, arg := m.clickActionAt(msg.X, msg.Y)
	if act == clickNone {
		return m, nil
	}
	return m.runClick(act, arg)
}

// handleMouseRelease сообщает терминалу об отпускании кнопки: без этого
// перетаскивание в vim залипает до следующего нажатия.
func (m tuiModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if !m.mouseEnabled() || m.state != tuiTerminal {
		return m, nil
	}
	if m.inTerminalScreen(msg.X, msg.Y) {
		m.forwardMouse(m.mouseEvent(vt.MouseRelease, vt.MouseLeft, msg.Mod, msg.X, msg.Y))
	}
	return m, nil
}

// handleMouseMotion передаёт движение указателя терминалу. Оно нужно только
// когда программа внутри окна попросила 1003: при включённой мыши dmsh без
// всякой пользы шлёт поток движений в терминал.
func (m tuiModel) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if !m.mouseEnabled() || m.state != tuiTerminal {
		return m, nil
	}
	if !m.inTerminalScreen(msg.X, msg.Y) {
		return m, nil
	}
	button := vt.MouseNone
	if msg.Button == tea.MouseLeft {
		button = vt.MouseLeft
	}
	m.forwardMouse(m.mouseEvent(vt.MouseMove, button, msg.Mod, msg.X, msg.Y))
	return m, nil
}

// handleMouseWheel отвечает на клик: находит область кадра и выполняет её
// действие. Колесо листает то, что сейчас на экране: список папок в окне
// выбора и скроллбек в остальных состояниях.
func (m tuiModel) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if !m.mouseEnabled() {
		return m, nil
	}
	if m.state == tuiTerminal {
		if !m.inTerminalScreen(msg.X, msg.Y) {
			return m, nil
		}
		button := vt.MouseWheelDown
		if msg.Button == tea.MouseWheelUp {
			button = vt.MouseWheelUp
		} else if msg.Button != tea.MouseWheelDown {
			return m, nil
		}
		m.forwardMouse(m.mouseEvent(vt.MousePress, button, msg.Mod, msg.X, msg.Y))
		return m, nil
	}
	step := 3
	switch msg.Button {
	case tea.MouseWheelUp:
		step = -step
	case tea.MouseWheelDown:
	default:
		return m, nil
	}
	if m.state == tuiDirMenu {
		m.dirMove(step, len(m.visibleDirItems()))
		return m, nil
	}
	m.scrollBy(step)
	return m, nil
}
