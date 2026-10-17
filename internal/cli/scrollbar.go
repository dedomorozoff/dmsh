package cli

import "github.com/charmbracelet/x/ansi"

// Полоса прокрутки — узкая колонка справа от транскрипта. Пока она не нужна
// (всё помещается), её нет вовсе: лишний символ в конце каждой строки на
// пустом месте — это цена, которую платят только те, кому есть что скроллить.
//
// Перетаскивания ползунка здесь нет намеренно: для этого терминал должен
// слать движение мыши при любом положении указателя (AllMotion), иначе он не
// приходит. Вместо этого клик по дорожке прокручивает страницу, а колесо
// листает построчно.

// scrollTrackMinRows — минимальная длина трека: на очень низком окне полоса
// из одной строки превращалась бы в набор случайных пикселей.
const scrollTrackMinRows = 3

// scrollGeom — геометрия полосы в кадре: строки дорожки и ползунка.
type scrollGeom struct {
	top, bottom        int // строки дорожки, включительно
	thumbTop, thumbBot int
	rows               int // сколько строк в транскрипте всего
}

// scrollbarGeom считает полосу по видимому окну транскрипта. ok = false,
// когда прокручивать нечего: полоса не рисуется и не кликается.
func (m tuiModel) scrollbarGeom(avail int) (scrollGeom, bool) {
	if m.width <= 0 || avail < scrollTrackMinRows {
		return scrollGeom{}, false
	}
	// Строки транскрипта известны из layoutRows: пересчитывать их ради
	// геометрии ещё раз дорого, а в кадре их и так видно.
	body, _ := m.layoutRows()
	total := len(body)
	maxOffset := total - avail
	if maxOffset <= 0 {
		return scrollGeom{}, false
	}
	g := scrollGeom{top: 0, bottom: avail - 1, rows: total}
	track := g.bottom - g.top + 1
	// Ползунок показывает, сколько экрана занято видимым куском.
	thumb := track * avail / total
	if thumb < 1 {
		thumb = 1
	}
	if thumb > track {
		thumb = track
	}
	// Дорожка идёт сверху вниз от старого лога к свежему, как в терминале и
	// редакторах: ползунок внизу, когда видно свежее (scrollOffset = 0), и
	// поднимается к началу, когда пользователь уходит в историю.
	g.thumbBot = g.bottom
	if travel := track - thumb; travel > 0 && maxOffset > 0 {
		g.thumbBot -= m.scrollOffset * travel / maxOffset
	}
	g.thumbTop = g.thumbBot - thumb + 1
	if g.thumbTop < g.top {
		g.thumbTop = g.top
		g.thumbBot = g.top + thumb - 1
	}
	return g, true
}

// cell возвращает символ полосы для строки кадра: ползунок, дорожку или
// пробел там, где полосы быть не должно.
func (g scrollGeom) cell(row int) string {
	switch {
	case row >= g.thumbTop && row <= g.thumbBot:
		return colorGray + "█" + colorReset
	case row >= g.top && row <= g.bottom:
		return gray + "│" + colorReset
	}
	return " "
}

// withScrollbar накладывает полосу на последнюю колонку строк транскрипта.
// Строка ввода и статусная строка не трогаются: полоса относится к выводу, а
// не к пульту.
func (m tuiModel) withScrollbar(frame []string, bodyCount int) []string {
	g, ok := m.scrollbarGeom(bodyCount)
	if !ok {
		return frame
	}
	out := make([]string, len(frame))
	copy(out, frame)
	for i := 0; i < bodyCount && i < len(out); i++ {
		if i >= g.top && i <= g.bottom {
			out[i] = ansi.Cut(out[i], 0, m.width-1) + g.cell(i)
		}
	}
	return out
}

// scrollToRow прокручивает транскрипт к строке полосы. Клик по дорожке
// двигает ползунок центром в точку клика: страница вместо построчной
// подстройки, как в браузере.
func (m tuiModel) scrollToRow(row int) tuiModel {
	g, ok := m.scrollbarGeom(m.scrollBodyRows())
	if !ok {
		return m
	}
	if row < g.top {
		row = g.top
	}
	if row > g.bottom {
		row = g.bottom
	}
	track := g.bottom - g.top + 1
	maxOffset := g.rows - track
	if maxOffset <= 0 {
		return m
	}
	// Верх дорожки — начало лога (maxOffset), низ — свежий вывод (0).
	// Умножение идёт первым: иначе целочисленное деление оставило бы на
	// нижней строке остаток и «вернуться вниз» было бы невозможно.
	span := max(1, g.bottom-g.top)
	m.scrollOffset = maxOffset * (g.bottom - row) / span
	return m
}

// scrollBodyRows — сколько строк кадра занимает транскрипт: столько же, во
// сколько строк он помещается.
func (m tuiModel) scrollBodyRows() int {
	_, inRows := m.layoutRows()
	avail := m.height - 1 - len(inRows)
	if avail < 0 {
		avail = 0
	}
	return avail
}

// scrollbarRegions — кликабельная дорожка полосы: по строке на дорожку,
// иначе клик вел бы в одну точку вместо всей полосы.
func (m tuiModel) scrollbarRegions() []clickRegion {
	g, ok := m.scrollbarGeom(m.scrollBodyRows())
	if !ok {
		return nil
	}
	regions := make([]clickRegion, 0, g.bottom-g.top+1)
	for row := g.top; row <= g.bottom; row++ {
		regions = append(regions, clickRegion{act: clickScrollTrack, arg: row, x0: m.width - 1, x1: m.width, y: row})
	}
	return regions
}
