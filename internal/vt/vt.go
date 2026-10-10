// Package vt рисует вывод оболочки в окне приложения.
//
// Это минимальный эмулятор терминала, а не полноценный. Он понимает то,
// чем реально пользуются оболочки и простые программы: перевод строки,
// возврат каретки, стирание строки и экрана, перемещение курсора и цвета
// SGR. Полноэкранные программы (vim, less) рисуются частично — сознательно:
// предсказуемый вывод лучше молча сломанного терминала.
//
// Экземпляр Screen не потокобезопасен: его пишет и читает только цикл
// обновления TUI, как и остальные буферы интерфейса.
package vt

import (
	"strings"
	"unicode/utf8"
)

// cell — одна ячейка экрана: символ и действующий на нём SGR-код.
type cell struct {
	r     rune
	style string
}

// Состояния парсера потока.
const (
	stGround = iota
	stEsc
	stCSI
	stOSC
	stString // DCS/PM/APC: пропускаем до ESC \
	stStrEsc // ESC внутри строки: ждём '\'
	stCharset
)

// Screen — экран терминала фиксированного размера. Вне его границ вывод
// прокручивается вверх: скроллбэка нет, терминал в окне приложения и нужен
// только как живой терминал, а не как просмотрщик истории.
type Screen struct {
	cols, rows int
	cells      [][]cell
	cx, cy     int
	style      string

	state  int
	params []byte
	// utf8buf держит недобранный UTF-8 на границе буферов чтения: символ
	// может прийти двумя кусками.
	utf8buf []byte
}

// NewScreen создаёт пустой экран cols x rows.
func NewScreen(cols, rows int) *Screen {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	s := &Screen{cols: cols, rows: rows}
	s.cells = make([][]cell, rows)
	for i := range s.cells {
		s.cells[i] = blankRow(cols)
	}
	return s
}

func blankRow(cols int) []cell {
	row := make([]cell, cols)
	for i := range row {
		row[i] = cell{r: ' '}
	}
	return row
}

// Size возвращает размер экрана в ячейках.
func (s *Screen) Size() (cols, rows int) { return s.cols, s.rows }

// Resize меняет размер экрана, сохраняя содержимое в общей части: оболочка
// перерисует себя по SIGWINCH сама, но промежуточное состояние не должно
// выглядеть как мусор.
func (s *Screen) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == s.cols && rows == s.rows {
		return
	}
	cells := make([][]cell, rows)
	for i := range cells {
		cells[i] = blankRow(cols)
	}
	for y := 0; y < rows && y < len(s.cells); y++ {
		copy(cells[y], s.cells[y][:min(cols, len(s.cells[y]))])
	}
	s.cells, s.cols, s.rows = cells, cols, rows
	s.cx = min(s.cx, cols-1)
	s.cy = min(s.cy, rows-1)
}

// Cursor возвращает позицию курсора в ячейках.
func (s *Screen) Cursor() (x, y int) { return s.cx, s.cy }

// Write разбирает поток оболочки. Ошибки не возвращаются: частичные
// escape-последовательности просто ждут следующего куска.
func (s *Screen) Write(p []byte) (int, error) {
	s.feed(p)
	return len(p), nil
}

// WriteString — короткая запись для строковых escape-последовательностей и
// результатов команд.
func (s *Screen) WriteString(p string) { s.feed([]byte(p)) }

func (s *Screen) feed(p []byte) {
	if len(s.utf8buf) > 0 {
		joined := make([]byte, 0, len(s.utf8buf)+len(p))
		joined = append(joined, s.utf8buf...)
		p = append(joined, p...)
		s.utf8buf = nil
	}
	for i := 0; i < len(p); {
		b := p[i]
		switch s.state {
		case stGround:
			i = s.feedGround(p, i)
		case stEsc:
			i++
			s.state = stGround
			s.feedEsc(b)
		case stCSI:
			switch {
			case b >= 0x40 && b <= 0x7e:
				i++
				s.csi(b, s.params)
				s.state = stGround
			case b >= 0x30 && b <= 0x3f:
				s.params = append(s.params, b)
				i++
			default:
				i++ // промежуточные байты: пропускаем
			}
		case stOSC, stString:
			switch b {
			case 0x07: // BEL
				i++
				s.state = stGround
			case 0x1b:
				i++
				s.state = stStrEsc
			default:
				i++
			}
		case stStrEsc:
			i++
			s.state = stGround
		case stCharset:
			i++
			s.state = stGround
		}
	}
}

// feedGround обрабатывает один байт обычного текста и возвращает позицию
// следующего.
func (s *Screen) feedGround(p []byte, i int) int {
	b := p[i]
	switch b {
	case 0x1b:
		s.state = stEsc
		return i + 1
	case '\r':
		s.cx = 0
		return i + 1
	case '\n':
		s.lineFeed()
		s.cx = 0
		return i + 1
	case '\b':
		if s.cx > 0 {
			s.cx--
		}
		return i + 1
	case '\t':
		s.tab()
		return i + 1
	}
	if b < 0x20 || b == 0x7f {
		return i + 1 // прочие управляющие символы игнорируем
	}
	r, size := utf8.DecodeRune(p[i:])
	if r == utf8.RuneError && size <= 1 {
		if !utf8.FullRune(p[i:]) {
			// символ разрезан границей буфера — ждём остаток
			s.utf8buf = append(s.utf8buf[:0], p[i:]...)
			return len(p)
		}
		return i + 1 // невалидный байт: пропускаем
	}
	s.put(r)
	return i + size
}

// feedEsc разбирает байт после ESC.
func (s *Screen) feedEsc(b byte) {
	switch b {
	case '[':
		s.params = s.params[:0]
		s.state = stCSI
	case ']', 'P', '^', '_':
		s.state = stOSC
	case '(', ')', '*', '+', '-', '.', '/':
		s.state = stCharset
	case 'D': // IND
		s.lineFeed()
	case 'E': // NEL
		s.lineFeed()
		s.cx = 0
	case 'M': // RI
		if s.cy > 0 {
			s.cy--
		}
	case 'c': // RIS
		s.reset()
	}
}

// csi применяет управляющую последовательность CSI.
func (s *Screen) csi(final byte, params []byte) {
	switch final {
	case 'm':
		s.style = sgrStyle(params)
	case 'A':
		s.cy = clamp(s.cy-csiNum(params, 0, 1), 0, s.rows-1)
	case 'B':
		s.cy = clamp(s.cy+csiNum(params, 0, 1), 0, s.rows-1)
	case 'C':
		s.cx = clamp(s.cx+csiNum(params, 0, 1), 0, s.cols-1)
	case 'D':
		s.cx = clamp(s.cx-csiNum(params, 0, 1), 0, s.cols-1)
	case 'E':
		s.cy = clamp(s.cy+csiNum(params, 0, 1), 0, s.rows-1)
		s.cx = 0
	case 'F':
		s.cy = clamp(s.cy-csiNum(params, 0, 1), 0, s.rows-1)
		s.cx = 0
	case 'G', '`':
		s.cx = clamp(csiNum(params, 0, 1)-1, 0, s.cols-1)
	case 'd':
		s.cy = clamp(csiNum(params, 0, 1)-1, 0, s.rows-1)
	case 'H', 'f':
		row, col := csiPair(params)
		s.cy = clamp(row-1, 0, s.rows-1)
		s.cx = clamp(col-1, 0, s.cols-1)
	case 'J':
		s.eraseDisplay(csiNum(params, 0, 0))
	case 'K':
		s.eraseLine(csiNum(params, 0, 0))
	case 'h', 'l', 'n', 'r', 's', 'u', 'X':
		// Видимость курсора, области скролла и сохранение позиции в окне
		// приложения не нужны: курсор всегда на своём месте.
	}
}

// put печатает символ в текущей позиции с переносом по краю экрана.
func (s *Screen) put(r rune) {
	if s.cx >= s.cols {
		s.cx = 0
		s.lineFeed()
	}
	s.cells[s.cy][s.cx] = cell{r: r, style: s.style}
	s.cx++
}

func (s *Screen) tab() {
	next := (s.cx/8 + 1) * 8
	if next > s.cols {
		next = s.cols
	}
	s.cx = next
}

func (s *Screen) lineFeed() {
	if s.cy < s.rows-1 {
		s.cy++
		return
	}
	copy(s.cells, s.cells[1:])
	s.cells[s.rows-1] = blankRow(s.cols)
}

func (s *Screen) eraseLine(mode int) {
	row := s.cells[s.cy]
	switch mode {
	case 0:
		s.eraseCells(row, s.cx, len(row))
	case 1:
		s.eraseCells(row, 0, s.cx+1)
	default:
		s.eraseCells(row, 0, len(row))
	}
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseCells(s.cells[s.cy], s.cx, s.cols)
		for y := s.cy + 1; y < s.rows; y++ {
			s.eraseCells(s.cells[y], 0, s.cols)
		}
	case 1:
		s.eraseCells(s.cells[s.cy], 0, s.cx+1)
		for y := 0; y < s.cy; y++ {
			s.eraseCells(s.cells[y], 0, s.cols)
		}
	default:
		for y := 0; y < s.rows; y++ {
			s.eraseCells(s.cells[y], 0, s.cols)
		}
	}
}

func (s *Screen) eraseCells(row []cell, from, to int) {
	for i := max(0, from); i < min(to, len(row)); i++ {
		row[i] = cell{r: ' '}
	}
}

func (s *Screen) reset() {
	for y := range s.cells {
		s.cells[y] = blankRow(s.cols)
	}
	s.cx, s.cy, s.style = 0, 0, ""
}

// Lines возвращает строки экрана с escape-последовательностями SGR: их
// отдаём в кадр как есть, иначе цвета оболочки теряются.
func (s *Screen) Lines() []string {
	out := make([]string, s.rows)
	for y := range s.cells {
		out[y] = s.line(y)
	}
	return out
}

// Text возвращает строки экрана без оформления — для тестов и отладки.
func (s *Screen) Text() []string {
	out := make([]string, s.rows)
	for y, row := range s.cells {
		out[y] = strings.TrimRight(string(runesOf(row)), " ")
	}
	return out
}

func (s *Screen) line(y int) string {
	row := s.cells[y]
	last := -1
	for i := len(row) - 1; i >= 0; i-- {
		if row[i].r != ' ' || row[i].style != "" {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	var b strings.Builder
	cur := ""
	for i := 0; i <= last; i++ {
		if row[i].style != cur {
			cur = row[i].style
			if cur == "" {
				b.WriteString("\x1b[0m")
			} else {
				b.WriteString(cur)
			}
		}
		b.WriteRune(row[i].r)
	}
	if cur != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func runesOf(row []cell) []rune {
	out := make([]rune, len(row))
	for i, c := range row {
		out[i] = c.r
	}
	return out
}

// sgrStyle превращает параметры SGR в готовую escape-последовательность.
// Семантику цветов знать не нужно: активный набор кодов переносится на
// следующую ячейку один в один.
func sgrStyle(params []byte) string {
	if len(params) == 0 {
		return "\x1b[0m"
	}
	return "\x1b[" + string(params) + "m"
}

// csiNum читает первый числовой параметр. Пустой или нечисловой параметр
// даёт значение по умолчанию — так требует стандарт терминалов.
func csiNum(params []byte, idx, def int) int {
	field := 0
	i := 0
	for ; i < len(params); i++ {
		if params[i] == ';' {
			if field == idx {
				return def
			}
			field++
			continue
		}
		if field != idx {
			continue
		}
		if params[i] < '0' || params[i] > '9' {
			return def
		}
		n := 0
		for ; i < len(params) && params[i] >= '0' && params[i] <= '9'; i++ {
			n = n*10 + int(params[i]-'0')
			if n > 1<<20 {
				return 1 << 20
			}
		}
		return n
	}
	return def
}

// csiPair читает пару параметров "строка;колонка" с позиционными
// значениями по умолчанию (1).
func csiPair(params []byte) (row, col int) {
	return csiNum(params, 0, 1), csiNum(params, 1, 1)
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }
