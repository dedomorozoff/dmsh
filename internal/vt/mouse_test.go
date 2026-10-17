package vt

import "testing"

// Программа внутри окна включает мышь себе — и до сих пор получал кантилевер:
// последовательности включения молча глотались.
func TestMouseModeFollowsAppRequest(t *testing.T) {
	tests := []struct {
		name  string
		seq   string
		track MouseTrack
	}{
		{"off by default", "", MouseOff},
		{"normal tracking", "\x1b[?1000h", MouseClick},
		{"drag tracking", "\x1b[?1002h", MouseDrag},
		{"any tracking", "\x1b[?1003h", MouseAny},
		{"reset", "\x1b[?1000h\x1b[?1000l", MouseOff},
		{"any covers drag", "\x1b[?1002h\x1b[?1003h", MouseAny},
		{"dropping any keeps drag", "\x1b[?1002h\x1b[?1003h\x1b[?1003l", MouseDrag},
		{"dropping all keeps click", "\x1b[?1000h\x1b[?1002h\x1b[?1002l", MouseClick},
		{"drag implies clicks", "\x1b[?1002h", MouseDrag},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScreen(20, 5)
			s.Write([]byte(tc.seq))
			if got := s.MouseMode().Track; got != tc.track {
				t.Fatalf("track = %d, want %d for %q", got, tc.track, tc.seq)
			}
		})
	}
}

// Формат кодирования выбирает приложение; по умолчанию — старый X10.
func TestMouseEncodingFormat(t *testing.T) {
	s := NewScreen(20, 5)
	if s.MouseMode().SGR {
		t.Fatal("SGR must be off until the app asks for it")
	}
	s.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !s.MouseMode().SGR {
		t.Fatal("1006 must switch the encoding to SGR")
	}
	// Включение формата не включает саму мышь.
	if s.MouseMode().Track == MouseOff {
		t.Fatal("1006 is only an encoding, not a tracking mode")
	}
}

// Обычные последовательности DECSET не должны превращаться в «просьбу о мыши».
func TestNonMousePrivateModeIgnored(t *testing.T) {
	s := NewScreen(20, 5)
	s.Write([]byte("\x1b[?25l\x1b[?7h\x1b[?1049h\x1b[?2004h"))
	if got := s.MouseMode().Track; got != MouseOff {
		t.Fatalf("track = %d, want off", got)
	}
}

func TestEncodeMouseSGR(t *testing.T) {
	mode := MouseMode{Track: MouseClick, SGR: true}
	tests := []struct {
		name string
		ev   MouseEvent
		want string
	}{
		{"press", MouseEvent{Action: MousePress, Button: MouseLeft, Col: 1, Row: 1}, "\x1b[<0;1;1M"},
		{"release", MouseEvent{Action: MouseRelease, Button: MouseLeft, Col: 5, Row: 3}, "\x1b[<0;5;3m"},
		{"right", MouseEvent{Action: MousePress, Button: MouseRight, Col: 10, Row: 2}, "\x1b[<2;10;2M"},
		{"ctrl+shift", MouseEvent{Action: MousePress, Button: MouseMiddle, Shift: true, Ctrl: true, Col: 2, Row: 2}, "\x1b[<21;2;2M"},
		{"wheel up", MouseEvent{Action: MousePress, Button: MouseWheelUp, Col: 4, Row: 4}, "\x1b[<64;4;4M"},
		{"wheel down", MouseEvent{Action: MousePress, Button: MouseWheelDown, Col: 4, Row: 4}, "\x1b[<65;4;4M"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(EncodeMouse(mode, tc.ev)); got != tc.want {
				t.Fatalf("EncodeMouse = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeMouseX10(t *testing.T) {
	mode := MouseMode{Track: MouseDrag}
	// Нажатие: 0 + 32, координаты со сдвигом 32.
	if got := string(EncodeMouse(mode, MouseEvent{Action: MousePress, Button: MouseLeft, Col: 1, Row: 1})); got != "\x1b[M !!" {
		t.Fatalf("press = %q", got)
	}
	// Движение помечается битом 32: код кнопки 0 + 32 = 64 ('@').
	if got := string(EncodeMouse(mode, MouseEvent{Action: MouseMove, Button: MouseLeft, Col: 3, Row: 4})); got != "\x1b[M@#$" {
		t.Fatalf("motion = %q", got)
	}
	// Старый формат не умеет координаты больше 223: лучше не слать вовсе,
	// чем отправить мусор.
	if got := EncodeMouse(mode, MouseEvent{Action: MousePress, Col: 300, Row: 1}); got != nil {
		t.Fatalf("out-of-range coordinates must not be sent, got %q", got)
	}
}

// Мышь выключена, движение не запрошено, координаты невалидны — слать нечего.
func TestEncodeMouseReturnsNothing(t *testing.T) {
	off := MouseMode{}
	if got := EncodeMouse(off, MouseEvent{Action: MousePress, Col: 1, Row: 1}); got != nil {
		t.Fatalf("mouse off must send nothing, got %q", got)
	}
	click := MouseMode{Track: MouseClick, SGR: true}
	if got := EncodeMouse(click, MouseEvent{Action: MouseMove, Col: 1, Row: 1}); got != nil {
		t.Fatalf("motion without a tracking mode must send nothing, got %q", got)
	}
	if got := EncodeMouse(click, MouseEvent{Action: MousePress, Col: 0, Row: 1}); got != nil {
		t.Fatalf("zero coordinates must send nothing, got %q", got)
	}
}

// В режиме 1002 движение без зажатой кнопки не отправляется, в 1003 —
// отправляется. Разница ровно в том, ради чего приложение их и выбирает.
func TestEncodeMouseDragVersusAny(t *testing.T) {
	moving := MouseEvent{Action: MouseMove, Button: MouseNone, Col: 2, Row: 2}
	if got := EncodeMouse(MouseMode{Track: MouseDrag, SGR: true}, moving); got != nil {
		t.Fatalf("1002 must ignore motion without a button, got %q", got)
	}
	if got := EncodeMouse(MouseMode{Track: MouseAny, SGR: true}, moving); string(got) != "\x1b[<3;2;2M" {
		t.Fatalf("1003 must report motion, got %q", got)
	}
}
