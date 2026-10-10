package cli

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// modalBox — содержимое модального окна: заголовок, строки и позиция
// курсора внутри них. Курсор всегда указывает на одну из строк содержимого
// (не считая рамки), поэтому рамку можно рисовать сколь угодно толстой,
// не пересчитывая раскладку.
type modalBox struct {
	title   string
	rows    []string
	cursorX int
	cursorY int
}

// modalBorder — ширина рамки окна в колонках терминала: "│ " слева и " │"
// справа.
const modalBorder = 4

// frameBoxLines оборачивает содержимое рамкой и подгоняет по ширине
// терминала. Возвращает готовые строки и координаты курсора внутри рамки.
func frameBoxLines(box modalBox, width int) ([]string, int, int) {
	// content — ширина текста между вертикальными сторонами рамки; frame —
	// полная ширина окна. Все строки рамки обязаны быть ширины frame: если
	// стороны будут уже заголовка, окно «поедет» и вылезет за терминал.
	content := displayWidth(box.title) + 2
	for _, r := range box.rows {
		if w := displayWidth(r); w > content {
			content = w
		}
	}
	if avail := width - modalBorder; avail > 0 && content > avail {
		content = avail
	}
	if content < 8 {
		content = 8
	}
	frame := content + modalBorder

	lines := make([]string, 0, len(box.rows)+2)
	top := "┌─ " + box.title + " "
	lines = append(lines, colorCyan+top+strings.Repeat("─", max(0, frame-displayWidth(top)-1))+"┐"+colorReset)
	for i, row := range box.rows {
		padded := fitWidth(row, content)
		if w := displayWidth(padded); w < content {
			padded += strings.Repeat(" ", content-w)
		}
		marker := " "
		if i == box.cursorY {
			marker = "▌"
		}
		lines = append(lines, colorCyan+"│"+colorReset+marker+padded+" "+colorCyan+"│"+colorReset)
	}
	lines = append(lines, colorCyan+"└"+strings.Repeat("─", max(0, frame-2))+"┘"+colorReset)

	// Курсор стоит на выбранной строке содержимого: левая граница рамки и
	// маркер занимают ровно две колонки.
	return lines, 2 + box.cursorX, 1 + box.cursorY
}

// overlayModal размещает окно по центру кадра. Возвращает новый кадр и
// позицию курсора в его координатах.
func overlayModal(frame []string, box modalBox, width, height int) ([]string, int, int) {
	if height <= 0 || len(frame) == 0 {
		return frame, 0, 0
	}
	lines, cursorX, cursorY := frameBoxLines(box, width)
	boxW := displayWidth(lines[0])
	boxH := len(lines)

	originX := max(0, (width-boxW)/2)
	if originX+boxW > width {
		originX = max(0, width-boxW)
	}
	originY := max(0, (height-boxH)/2)

	out := make([]string, len(frame))
	copy(out, frame)
	for i := 0; i < boxH && originY+i < len(out); i++ {
		row := originY + i
		// Куски исходной строки по бокам окна вырезаются по колонкам,
		// а не по байтам: срез по байтам попал бы внутрь escape-последовательности
		// и оставил бы в кадре огрызок цвета.
		left := strings.Repeat(" ", originX)
		right := strings.Repeat(" ", max(0, width-originX-boxW))
		rowWidth := displayWidth(out[row])
		if originX > 0 && originX < rowWidth {
			left = ansi.Cut(out[row], 0, originX)
		}
		if cut := originX + boxW; cut < rowWidth {
			right = ansi.Cut(out[row], cut, width)
		}
		out[row] = left + lines[i] + right
	}
	return out, originX + cursorX, originY + cursorY
}