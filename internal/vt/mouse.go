package vt

import (
	"fmt"
	"strconv"
	"strings"
)

// Мышь внутри окна терминала — отдельная история. Терминал пользователя не
// отдаёт события мыши приложению, которое само их не включило: пока dmsh не
// просит, события уходят в его собственный интерфейс. Программа внутри
// псевдотерминала (vim, less, fzf, lazygit) включает мышь у себя — и эти
// последовательности раньше просто глотались, а приложение получало кантилевер
// без единого события. Теперь vt запоминает просьбу, а dmsh пересылает
// события в псевдотерминал сам.

// MouseTrack — уровень отслеживания мыши, который запросила программа.
type MouseTrack int

const (
	MouseOff   MouseTrack = iota // мышь не нужна
	MouseClick                   // 1000: нажатие и отпускание
	MouseDrag                    // 1002: движение с зажатой кнопкой
	MouseAny                     // 1003: любое движение
)

// MouseMode — что и в каком виде терминал внутри окна хочет получать.
type MouseMode struct {
	Track MouseTrack
	// SGR — формат 1006 (и близкий 1015). Без него действует старый
	// X10, в котором координаты ограничены 223.
	SGR bool
}

// WantsMotion — нужно ли движение мыши без нажатой кнопки (режим 1003).
func (m MouseMode) WantsMotion() bool { return m.Track == MouseAny }

// privateMouse — флаги DECSET/DECRST по мыши. Хранятся раздельно, потому что
// режимы вложены: 1003 включает и 1002, а сброс 1003 возвращает 1002, а не
// «мышь выключена».
type privateMouse struct {
	click bool // 1000
	drag  bool // 1002
	any   bool // 1003
}

// setPrivateMouse обрабатывает один из частных режимов мыши.
func (p *privateMouse) setPrivateMouse(mode int, on bool) {
	switch mode {
	case 1000:
		p.click = on
	case 1002:
		p.drag = on
	case 1003:
		p.any = on
	}
}

// mouseMode собирает режим из флагов: более широкий запрос перекрывает узкий.
func (p privateMouse) mouseMode(sgr bool) MouseMode {
	mode := MouseMode{SGR: sgr}
	switch {
	case p.any:
		mode.Track = MouseAny
	case p.drag:
		mode.Track = MouseDrag
	case p.click:
		mode.Track = MouseClick
	}
	return mode
}

// applyPrivateMode разбирает CSI ? Ps h / CSI ? Ps l. Значение mode — 0, если
// параметр не число: такая последовательность к мыши отношения не имеет.
func (s *Screen) applyPrivateMode(params []byte, on bool) {
	if len(params) == 0 || params[0] != '?' {
		return
	}
	for _, part := range strings.Split(string(params[1:]), ";") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		switch n {
		case 1000, 1002, 1003:
			s.mouse.setPrivateMouse(n, on)
		case 1006, 1015:
			// Формат кодирования: 1015 отличается от 1006 только отчётом
			// об отпускании, на что приложения не опираются.
			s.mouseSGR = on
		}
	}
}

// MouseMode возвращает просьбу терминала о мыши: dmsh включает свою мышь
// ровно на это время и пересылает события в псевдотерминал.
func (s *Screen) MouseMode() MouseMode {
	return s.mouse.mouseMode(s.mouseSGR)
}

// MouseAction — что случилось с кнопкой.
type MouseAction int

const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMove
)

// MouseButton — кнопка мыши в терминальном понимании.
type MouseButton int

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
)

// MouseEvent — событие мыши в координатах терминала: нумерация с единицы, как
// её ждёт программа внутри псевдотерминала.
type MouseEvent struct {
	Action MouseAction
	Button MouseButton
	Shift  bool
	Alt    bool
	Ctrl   bool
	Col    int
	Row    int
}

// EncodeMouse переводит событие в то, что ждёт терминал внутри окна. Пустой
// результат означает «нечего слать»: мышь выключена, режим не требует
// события этого вида или координаты не влезают в старый формат.
func EncodeMouse(mode MouseMode, ev MouseEvent) []byte {
	if mode.Track == MouseOff || ev.Col < 1 || ev.Row < 1 {
		return nil
	}
	if ev.Action == MouseMove && mode.Track == MouseClick {
		return nil
	}
	if ev.Action == MouseMove && mode.Track == MouseDrag && ev.Button == MouseNone {
		// Движение без зажатой кнопки в режиме 1002 не отправляется.
		return nil
	}
	button := ev.buttonCode()
	if mode.SGR {
		final := byte('M')
		if ev.Action == MouseRelease {
			final = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, ev.Col, ev.Row, final))
	}
	// Старый формат: координаты не больше 223, отпускание не отличается от
	// нажатия, движение помечается битом 32.
	if ev.Col > 223 || ev.Row > 223 {
		return nil
	}
	if ev.Action == MouseMove {
		button += 32
	}
	return []byte{'\x1b', '[', 'M', byte(button + 32), byte(ev.Col + 32), byte(ev.Row + 32)}
}

func (ev MouseEvent) buttonCode() int {
	code := 0
	switch ev.Button {
	case MouseLeft:
		code = 0
	case MouseMiddle:
		code = 1
	case MouseRight:
		code = 2
	case MouseWheelUp:
		code = 64
	case MouseWheelDown:
		code = 65
	default:
		code = 3
	}
	if ev.Shift {
		code += 4
	}
	if ev.Alt {
		code += 8
	}
	if ev.Ctrl {
		code += 16
	}
	return code
}
