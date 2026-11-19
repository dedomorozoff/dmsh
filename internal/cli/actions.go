package cli

import (
	"fmt"

	"charm.land/bubbletea/v2"
)

// После выполненной команды dmsh не знает, что делать дальше, а пользователь
// обычно знает: повторить, разобрать или просто открыть оболочку и посмотреть
// своими глазами. Панель превращает это «знание» в три кнопки под ответом.
//
// Панель живёт в блоке строки ввода, а не в транскрипте: координаты кнопок
// тогда не зависят от прокрутки, а сам ряд виден всегда, пока есть что
// предлагать.

// actionBar — кнопки под ответом. label — что на кнопке, cmd — команда,
// которую она выполняет (пусто, если действие не в списке слэш-команд).
type actionBar struct {
	label   string
	cmd     string
	explain bool // нажать = подставить запрос в строку ввода
}

var actionBarItems = []actionBar{
	{label: "retry", cmd: "/retry"},
	{label: "explain", explain: true},
	{label: "shell", cmd: "/shell"},
}

var actionBarKeys = []string{"alt+r", "alt+e", "alt+s"}

func actionBarKeyLabel(idx int) string {
	switch idx {
	case 0:
		return "Alt+R"
	case 1:
		return "Alt+E"
	case 2:
		return "Alt+S"
	default:
		return ""
	}
}

// showActionBar — нужна ли панель. Показываем только после выполненной
// команды: перед первым запросом предлагать «повторить» бессмысленно, а
// постоянная строка в кадре стоила бы места зря.
func (m tuiModel) showActionBar() bool {
	return m.actionCmd != "" && m.state == tuiIdle && !m.running && !m.streaming
}

// actionBarSegments — кнопки панели с их границами: клик и отрисовка идут по
// одному списку, иначе кнопка уедет на соседнюю.
func (m tuiModel) actionBarSegments() []statusSegment {
	if !m.showActionBar() {
		return nil
	}
	segs := make([]statusSegment, 0, len(actionBarItems)*2)
	for i, item := range actionBarItems {
		if i > 0 {
			segs = append(segs, statusSegment{text: "   "})
		}
		label := item.label
		if hint := actionBarKeyLabel(i); hint != "" {
			label = fmt.Sprintf("%s %s%s%s", item.label, colorGray, hint, colorCyan)
		}
		segs = append(segs, statusSegment{
			text: fmt.Sprintf("%s[ %s ]%s", colorCyan, label, colorReset),
			act:  clickRetry + clickAction(i),
		})
	}
	return segs
}

// actionBarRow — строка панели целиком.
func (m tuiModel) actionBarRow() string {
	return segmentsText(m.actionBarSegments())
}

// actionBarRegions — кликабельные кнопки панели.
func (m tuiModel) actionBarRegions() []clickRegion {
	segs := m.actionBarSegments()
	if len(segs) == 0 {
		return nil
	}
	// Action bar is rendered immediately after prompt/input rows in the default layout.
	promptRows := len(hardWrap(m.buildPrompt()+m.input, m.width))
	if promptRows < 1 {
		promptRows = 1
	}
	y := m.inputRow() + promptRows
	regions := make([]clickRegion, 0, len(segs))
	x := 0
	for _, s := range segs {
		w := displayWidth(s.text)
		if s.act != clickNone {
			regions = append(regions, clickRegion{act: s.act, x0: x, x1: x + w, y: y})
		}
		x += w
	}
	return regions
}

// runActionBar выполняет нажатую кнопку. Каждая кнопка делает ровно то же,
// что и её слэш-команда: мышь — это короткий путь, а не второй интерфейс.
func (m tuiModel) runActionBar(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(actionBarItems) {
		return m, nil
	}
	item := actionBarItems[idx]
	if item.explain {
		// Объяснение — это запрос модели, а не команда оболочки: подставляем
		// его в строку ввода и оставляем отправку пользователю.
		m.input = fmt.Sprintf("explain what `%s` does and what it changes", m.actionCmd)
		m.cursorPos = runeSliceLen(m.input)
		return m, nil
	}
	// Команда выполняется тем же путём, что и напечатанная: один набор
	// обработчиков на оба способа ввода.
	return m.handleSlash(item.cmd)
}
