package cli

import (
	"strings"

	"charm.land/bubbletea/v2"
)

// clickAction — что делает клик в найденной области. Кликабельность описана
// одним списком и проверяется тестами по точкам на отрисованном кадре:
// «кнопка нарисована, но клика не ловит» — самый частый способ испортить
// такой интерфейс.
type clickAction int

const (
	clickNone          clickAction = iota
	clickOpenDir                   // папка в статусной строке → выбор каталога
	clickOpenModeMenu              // режим в статусной строке → список режимов
	clickOpenModelMenu             // модель в статусной строке → меню моделей
	clickOpenSettings              // proxy в статусной строке → настройки сети
	clickShowHelp                  // подсказка help
	clickOpenPalette               // подсказка palette
	clickCycleMode                 // подсказка mode Shift+Tab
	clickDirItem                   // пункт окна выбора папки
	clickModeItem                  // пункт списка режимов
	clickPaletteItem               // пункт панели команд
	clickModelItem                 // пункт меню моделей
	clickSettingRow                // строка экрана настроек
	clickSetupRow                  // строка экрана setup
	clickJumpToEnd                 // «↓N below» в строке фоновой команды
	clickConfirmRun                // кнопка «run» в окне подтверждения
	clickConfirmCancel             // кнопка «cancel» там же
	clickTodoItem                  // задача в окне todo: отметить/снять
	clickScrollTrack               // дорожка полосы прокрутки
	clickRetry                     // [ retry ] под ответом
	clickExplain                   // [ explain ] там же
	clickOpenShell                 // [ shell ] там же
)

// statusSegment — кусочек статусной строки: текст и, если он кликабельный,
// действие. Пустое действие означает разделитель вроде двух пробелов.
type statusSegment struct {
	text string
	act  clickAction
}

func segmentsWidth(segs []statusSegment) int {
	w := 0
	for _, s := range segs {
		w += displayWidth(s.text)
	}
	return w
}

func segmentsText(segs []statusSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// clickRegion — прямоугольник кадра, который что-то делает. Полуинтервал
// колонок [x0, x1) и строка y.
type clickRegion struct {
	act    clickAction
	arg    int
	x0, x1 int
	y      int
}

// statusRegions — кликабельные области статусной строки. Правая половина
// (подсказки) может не поместиться в терминал: тогда она не рисуется, не
// кликается и в список не попадает.
func (m tuiModel) statusRegions() []clickRegion {
	if m.width <= 0 || m.height < 1 {
		return nil
	}
	left, right := m.statusSegments()
	row := m.height - 1
	regions := make([]clickRegion, 0, len(left)+len(right))
	add := func(segs []statusSegment, x int) {
		for _, s := range segs {
			w := displayWidth(s.text)
			if s.act != clickNone && w > 0 && x < m.width {
				// Обрезанный fitWidth'ом кусок не нарисован целиком, но его
				// начало — вполне кликабельно.
				regions = append(regions, clickRegion{act: s.act, x0: x, x1: min(x+w, m.width), y: row})
			}
			x += w
		}
	}
	add(left, 0)
	// Пустое место между половинами считается так же, как при отрисовке.
	pad := m.width - segmentsWidth(left) - segmentsWidth(right)
	if pad < 1 {
		pad = 1
	}
	add(right, segmentsWidth(left)+pad)
	return regions
}

// statusActionAt — действие по клику в статусной строке.
func (m tuiModel) statusActionAt(x, y int) (clickAction, int) {
	for _, r := range m.statusRegions() {
		if r.y == y && x >= r.x0 && x < r.x1 {
			return r.act, r.arg
		}
	}
	return clickNone, 0
}

// modalRegions — кликабельные строки открытого окна. Рамка и подвал в список
// не попадают: промах мимо строки ничего не делает, закрывать окно вслепую
// не должен даже клик по рамке.
func (m tuiModel) modalRegions() []clickRegion {
	box := m.modalBox()
	lines, _, _ := frameBoxLines(box, m.width)
	boxW := displayWidth(lines[0])
	regions := make([]clickRegion, 0, len(box.rows))
	for row := 0; row < len(box.rows); row++ {
		act, arg := m.modalRowAction(row)
		if act == clickNone {
			continue
		}
		x, y, ok := m.modalRowPoint(row)
		if !ok {
			continue
		}
		// Строка кликабельна от маркера до правой стороны рамки: за окном
		// клик должен промахиваться, как и по рамке.
		regions = append(regions, clickRegion{act: act, arg: arg, x0: x, x1: min(x+boxW, m.width), y: y})
	}
	return regions
}

// modelItemRegions — кликабельные пункты меню моделей. Меню не окно, а строки
// на месте строки ввода, поэтому позиция считается от inputRow.
func (m tuiModel) modelItemRegions() []clickRegion {
	_, first := m.menuRows()
	start, end := m.menuWindow()
	regions := make([]clickRegion, 0, end-start)
	for i := start; i < end; i++ {
		regions = append(regions, clickRegion{
			act: clickModelItem, arg: i,
			x0: 0, x1: m.width,
			y: m.inputRow() + first + i - start,
		})
	}
	return regions
}

// jumpToEndRegions — клик по строке фоновой команды возвращает в конец
// лога. Показывается только когда пользователь ушёл читать выше: иначе
// прыгать некуда и строка была бы просто украшением.
func (m tuiModel) jumpToEndRegions() []clickRegion {
	if m.state != tuiRunning || m.scrollOffset < 1 {
		return nil
	}
	row := m.runningRow()
	if row < 0 {
		return nil
	}
	return []clickRegion{{act: clickJumpToEnd, x0: 0, x1: m.width, y: row}}
}

// runningRow — номер строки ввода, где показан «… running», или -1. Ищется по
// той же строке, которую рисует layoutRows: отдельная формула рано или поздно
// разошлась бы с раскладкой.
func (m tuiModel) runningRow() int {
	_, inRows := m.layoutRows()
	line := m.runningLine()
	for i, r := range inRows {
		if r == line {
			return m.inputRow() + i
		}
	}
	return -1
}

// clickRegions — все кликабельные области текущего экрана. Окно проверяется
// раньше статусной строки: на маленьком терминале окно накрывает её, и клик
// должен попадать в окно, а не в то, что под ним.
func (m tuiModel) clickRegions() []clickRegion {
	// В режиме терминала мышь принадлежит оболочке внутри окна.
	if m.state == tuiTerminal || m.width <= 0 || m.height <= 0 {
		return nil
	}
	if _, open := m.modal(); open {
		return m.modalRegions()
	}
	if m.state == tuiModelMenu {
		return m.modelItemRegions()
	}
	return append(append(append(m.statusRegions(), m.scrollbarRegions()...), m.jumpToEndRegions()...), m.actionBarRegions()...)
}

// clickActionAt — что означает клик в точке кадра.
func (m tuiModel) clickActionAt(x, y int) (clickAction, int) {
	for _, r := range m.clickRegions() {
		if r.y == y && x >= r.x0 && x < r.x1 {
			return r.act, r.arg
		}
	}
	return clickNone, 0
}

// modalRowPoint — координаты клика по строке содержимого окна (без рамки).
// Раскладку считает ровно та же, что и отрисовка, через modalOrigin.
func (m tuiModel) modalRowPoint(row int) (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	lines, _, _ := frameBoxLines(m.modalBox(), m.width)
	originX, originY := modalOrigin(displayWidth(lines[0]), len(lines), m.width, m.height)
	if row < 0 || row > len(lines)-3 {
		// Верхняя и нижняя стороны рамки не содержат строк.
		return 0, 0, false
	}
	// Колонка 2 — сразу за левой стороной рамки и маркером строки.
	return originX + 2, originY + 1 + row, true
}

// modalRowAction — что делает клик по строке содержимого окна. Правило одно
// на все окна: клик делает то же, что Enter на этой строке. Правило
// редактирующих строк — исключение, о нём сказано в modalRowClickable.
func (m tuiModel) modalRowAction(row int) (clickAction, int) {
	if !m.modalRowClickable(row) {
		return clickNone, 0
	}
	switch m.state {
	case tuiSettings:
		return clickSettingRow, row
	case tuiSetup:
		return clickSetupRow, row
	case tuiModeMenu:
		if row < len(modeOrder) {
			return clickModeItem, row
		}
	case tuiPalette:
		l := m.paletteLayout()
		if i := row - l.firstRowIdx + l.start; i >= l.start && i < l.end {
			return clickPaletteItem, i
		}
	case tuiDirMenu:
		l := m.dirMenuLayout()
		if i := row - l.firstRowIdx + l.start; i >= l.start && i < l.end {
			return clickDirItem, i
		}
	case tuiConfirming:
		return m.confirmClick(row)
	case tuiTodoMenu:
		l := m.todoLayout()
		if i := row - l.firstRowIdx + l.start; i >= l.start && i < l.end {
			return clickTodoItem, i
		}
	}
	return clickNone, 0
}

// modalRowClickable отсекает строки, которые кликом трогать нельзя: во время
// правки поля это строка ввода, а не значение, и Enter там завершает
// редактирование — потеря напечатанного была бы обидной ошибкой.
func (m tuiModel) modalRowClickable(row int) bool {
	switch m.state {
	case tuiSettings:
		return row >= 0 && row < setRowCount && !m.setEditing
	case tuiSetup:
		return row >= 0 && row < supRowCount && !m.supEditing
	}
	return row >= 0
}

// modelItemAt — пункт меню моделей под курсором или -1.

// runClick выполняет действие клика. Правило одно: каждая кнопка делает то
// же, что Enter или соответствующая клавиша, — иначе мышь и клавиатура
// разъедутся, и это замечают не сразу.
func (m tuiModel) runClick(act clickAction, arg int) (tea.Model, tea.Cmd) {
	switch act {
	case clickOpenDir:
		return m.openDirMenu(), nil
	case clickOpenModeMenu:
		return m.openModeMenu(), nil
	case clickOpenModelMenu:
		mm, cmd := m.openModelMenu()
		return mm, cmd
	case clickOpenSettings:
		return m.openSettings(), nil
	case clickShowHelp:
		m.showHelp()
		return m, nil
	case clickOpenPalette:
		return m.openPalette(), nil
	case clickCycleMode:
		return m.cycleMode()
	case clickDirItem:
		items := m.visibleDirItems()
		if arg < 0 || arg >= len(items) {
			return m, nil
		}
		return m.changeDir(items[arg].path)
	case clickModeItem:
		if arg < 0 || arg >= len(modeOrder) {
			return m, nil
		}
		return m.closeModeMenu().switchMode(modeOrder[arg])
	case clickPaletteItem:
		if arg < len(paletteMatches(m.input)) {
			m.paletteIdx = arg
		}
		return m.runPaletteItem()
	case clickModelItem:
		m.modelIdx = arg
		return m.menuEnter()
	case clickSettingRow:
		m.setIdx = arg
		return m.settingsEnter()
	case clickSetupRow:
		m.supIdx = arg
		return m.setupEnter()
	case clickJumpToEnd:
		m.jumpToEnd()
		return m, nil
	case clickConfirmRun:
		return m.applyConfirm(true)
	case clickConfirmCancel:
		return m.applyConfirm(false)
	case clickTodoItem:
		m.todoIdx = arg
		return m.toggleTodoAt(arg, m.todoItems()), nil
	case clickScrollTrack:
		return m.scrollToRow(arg), nil
	case clickRetry, clickExplain, clickOpenShell:
		return m.runActionBar(int(act - clickRetry))
	}
	return m, nil
}
