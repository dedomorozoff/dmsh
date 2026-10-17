package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/vt"
)

// Режим терминала живёт внутри окна dmsh: оболочка запущена в
// псевдотерминале, её вывод рисуется в кадре приложения, а нажатия клавиш
// уходят в псевдотерминал. Отдельная оболочка поверх TUI требовала отдать
// терминал и терять окно, поэтому режим включается и выключается Shift+Tab
// вместе с остальными.

// terminalPane — встроенный терминал: псевдотерминал и экран, в который
// TUI рисует его вывод.
type terminalPane struct {
	term   *executor.TerminalSession
	screen *vt.Screen
	outCh  chan terminalOutputMsg
	exitCh chan terminalExitMsg
	done   chan struct{}

	closeOnce sync.Once
}

// terminalOutputMsg — кусок вывода оболочки.
type terminalOutputMsg struct {
	data []byte
}

// terminalExitMsg — оболочка завершилась.
type terminalExitMsg struct {
	code int
	err  error
}

// terminalClosedMsg — панель закрыта (Shift+Tab, Ctrl+Q, выход оболочки).
// Нужен, чтобы висящая команда чтения отпустила канал и не держала
// горутину после закрытия терминала.
type terminalClosedMsg struct{}

// terminalReadChunk — размер порции, которую TUI читает из псевдотерминала.
const terminalReadChunk = 8192

// startTerminal открывает терминал в окне приложения. Терминал пользователя
// при этом остаётся у TUI, поэтому вызов безопасно делать из обработчика
// клавиш: Update не блокируется.
func (m tuiModel) startTerminal() (tuiModel, tea.Cmd) {
	if m.term != nil {
		return m, nil
	}
	if strings.TrimSpace(m.rf.cfg.Shell) == "" {
		m.addLine(fmt.Sprintf("%sno shell configured%s", colorRed, colorReset))
		return m, nil
	}
	cols, rows := m.terminalSize()
	cwd, _ := os.Getwd()
	opts := executor.TerminalOptsFromShell(m.rf.cfg.Shell, executor.TerminalSize{Cols: cols, Rows: rows})
	opts.Dir = cwd

	term, err := executor.StartTerminal(opts)
	if err != nil {
		m.addLine(fmt.Sprintf("%sterminal: %v%s", colorRed, err, colorReset))
		return m, nil
	}
	pane := newTerminalPane(term, cols, rows)
	go pane.read()
	go pane.wait()

	m.term = pane
	m.state = tuiTerminal
	return m, waitTerminal(pane)
}

// newTerminalPane собирает терминальную панель. term может быть nil: окно
// тогда пустое, но живое — так режим выглядит после неудачного запуска
// оболочки, и закрывать его всё равно нужно безопасно.
func newTerminalPane(term *executor.TerminalSession, cols, rows int) *terminalPane {
	return &terminalPane{
		term:   term,
		screen: vt.NewScreen(cols, rows),
		outCh:  make(chan terminalOutputMsg, 32),
		exitCh: make(chan terminalExitMsg, 1),
		done:   make(chan struct{}),
	}
}

// terminalSize возвращает размер области терминала в ячейках: последняя
// строка кадра занята статусом.
func (m tuiModel) terminalSize() (cols, rows int) {
	cols, rows = m.width, m.height-1
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return cols, rows
}

// resizeTerminal подгоняет терминал под текущий размер окна.
func (m *tuiModel) resizeTerminal() {
	if m.term == nil || m.term.term == nil {
		return
	}
	cols, rows := m.terminalSize()
	m.term.screen.Resize(cols, rows)
	_ = m.term.term.Resize(executor.TerminalSize{Cols: cols, Rows: rows})
}

// stopTerminal закрывает встроенный терминал и возвращает ввод в строку
// режима. Псевдотерминал закрывается ровно один раз — это требование ConPTY.
func (m tuiModel) stopTerminal(reason string) tuiModel {
	if m.term != nil {
		m.term.close()
		m.term = nil
	}
	if m.state == tuiTerminal {
		m.state = tuiIdle
	}
	if reason != "" {
		m.addLine(reason)
	}
	return m
}

// read читает вывод оболочки и отдаёт его в цикл обновления. Канал с
// буфером даёт естественное обратное давление: если TUI не успевает
// перерисовывать, чтение притормаживает, а не съедает память.
func (p *terminalPane) read() {
	buf := make([]byte, terminalReadChunk)
	for {
		n, err := p.term.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case p.outCh <- terminalOutputMsg{data: data}:
			case <-p.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// wait ждёт выхода оболочки отдельной горутиной: Wait блокируется до конца
// программы, а TUI в это время живёт своей жизнью.
func (p *terminalPane) wait() {
	code, err := p.term.Wait()
	select {
	case p.exitCh <- terminalExitMsg{code: code, err: err}:
	case <-p.done:
	}
}

// close завершает панель. Псевдотерминал закрывается ровно один раз:
// повторный Close у ConPTY закрывает уже закрытые хэндлы.
func (p *terminalPane) close() {
	p.closeOnce.Do(func() {
		close(p.done)
		if p.term != nil {
			_ = p.term.Close()
		}
	})
}

// writeKey отправляет нажатие в оболочку.
func (p *terminalPane) writeKey(msg tea.KeyPressMsg) {
	if p.term == nil {
		return
	}
	data := terminalKeyBytes(msg)
	if len(data) == 0 {
		return
	}
	_, _ = p.term.Write(data)
}

// writeText отправляет вставку из буфера обмена. Перевод строки приводится к
// CR: оболочка в raw-режиме ждёт именно его, иначе многострочная вставка
// слиплась бы в одну строку и не отправилась.
func (p *terminalPane) writeText(text string) {
	if p.term == nil || text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	_, _ = p.term.Write([]byte(strings.ReplaceAll(text, "\n", "\r")))
}

// waitTerminal качает вывод терминала в цикл обновления TUI. Пока панель
// открыта, она перевзводится после каждого сообщения — в том числе когда
// терминал нарисован под модальным окном, иначе вывод перестал бы приходить
// после первого открытия палитры.
func waitTerminal(pane *terminalPane) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-pane.outCh:
			return msg
		case msg := <-pane.exitCh:
			return msg
		case <-pane.done:
			return terminalClosedMsg{}
		}
	}
}

// terminalKeyBytes переводит нажатие в байты, которые ждёт терминал.
// Неописанные клавиши уходят в оболочку как есть.
func terminalKeyBytes(msg tea.KeyPressMsg) []byte {
	switch msg.Code {
	case tea.KeyEnter, tea.KeyKpEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	}
	// Ctrl+буква — это байт из диапазона 1..26. Shift и CapsLock на нём
	// ничего не меняют, а Ctrl+C в терминале принадлежит оболочке.
	if msg.Mod&tea.ModCtrl != 0 {
		switch r := msg.Code; {
		case r >= 'a' && r <= 'z':
			return []byte{byte(r - 'a' + 1)}
		case r >= 'A' && r <= 'Z':
			return []byte{byte(r - 'A' + 1)}
		}
	}
	if isInputKey(msg) && msg.Text != "" {
		return []byte(msg.Text)
	}
	return nil
}

// handleTerminalKey отправляет клавиши в терминал. Shift+Tab выводит из
// режима: иначе из терминала не выйти, не закрыв оболочку.
func (m tuiModel) handleTerminalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0 {
		return m.cycleMode()
	}
	if m.term == nil {
		return m.stopTerminal(""), nil
	}
	m.term.writeKey(msg)
	return m, nil
}

// finishTerminal печатает результат выхода оболочки и возвращает ввод в
// обычный режим: оставаться в режиме терминала без терминала нельзя.
func (m tuiModel) finishTerminal(msg terminalExitMsg) tuiModel {
	switch {
	case msg.err != nil:
		m.addLine(fmt.Sprintf("%sterminal: %v%s", colorRed, msg.err, colorReset))
	case msg.code != 0:
		m.addLine(fmt.Sprintf("%s[shell exited with code %d]%s", colorYellow, msg.code, colorReset))
	default:
		m.addLine(fmt.Sprintf("%s[shell exited]%s", colorGray, colorReset))
	}
	if m.rf.cfg.Mode == config.ModeShell {
		// Без терминала режим terminal не имеет смысла: следующий ввод
		// ушёл бы в пустоту.
		m.rf.cfg.Mode = config.ModeAI
		m.s.cfg.Mode = config.ModeAI
		m.modeLabel = modeLabel(config.ModeAI)
	}
	// Выход из оболочки считаем командой в истории: пользователь наверняка
	// что-то делал, и в /history это должно быть видно.
	m.s.addRecentAndHistory(m.rf.cfg.Shell+" (interactive)", "direct")
	return m.stopTerminal("")
}

// terminalRows рисует экран встроенного терминала.
func (m tuiModel) terminalRows() []string {
	if m.term == nil {
		return nil
	}
	return m.term.screen.Lines()
}

// terminalCursor возвращает позицию курсора терминала в кадре.
func (m tuiModel) terminalCursor() (tea.Position, bool) {
	if m.term == nil {
		return tea.Position{}, false
	}
	x, y := m.term.screen.Cursor()
	return tea.Position{X: x, Y: y}, true
}

// terminalOrigin — левый верхний угол экрана терминала в кадре. Считается той
// же арифметикой, что и renderFrame: строки терминала занимают блок
// скроллбека, и расхождение сдвинуло бы клик на другую строку оболочки.
func (m tuiModel) terminalOrigin() (x, y int) {
	if m.height <= 0 {
		return 0, 0
	}
	avail := m.height - 1
	total := len(m.terminalRows())
	start := total - avail - m.scrollOffset
	if start < 0 {
		start = 0
	}
	return 0, start
}

// mouseMode терминала — то, что просит программа внутри псевдотерминала.
// Пока она молчит, мышь остаётся у интерфейса dmsh.
func (m tuiModel) terminalMouseMode() vt.MouseMode {
	if m.term == nil || m.state != tuiTerminal {
		return vt.MouseMode{}
	}
	return m.term.screen.MouseMode()
}

// forwardMouse пересылает событие мыши в псевдотерминал. Нажатия dmsh не
// выполняет: пока программа внутри окна не включила мышь, клики остаются у
// интерфейса, а включила — значит, хочет их сама.
func (m tuiModel) forwardMouse(ev vt.MouseEvent) {
	if m.term == nil || m.term.term == nil {
		return
	}
	data := vt.EncodeMouse(m.terminalMouseMode(), ev)
	if len(data) == 0 {
		return
	}
	_, _ = m.term.term.Write(data)
}

// mouseEvent переводит событие bubbletea в координаты псевдотерминала.
func (m tuiModel) mouseEvent(action vt.MouseAction, button vt.MouseButton, mod tea.KeyMod, x, y int) vt.MouseEvent {
	ox, oy := m.terminalOrigin()
	return vt.MouseEvent{
		Action: action,
		Button: button,
		Shift:  mod&tea.ModShift != 0,
		Alt:    mod&tea.ModAlt != 0,
		Ctrl:   mod&tea.ModCtrl != 0,
		Col:    x - ox + 1,
		Row:    y - oy + 1,
	}
}

// inTerminalScreen сообщает, попал ли клик в экран терминала, а не в строку
// состояния или в поля вокруг него.
func (m tuiModel) inTerminalScreen(x, y int) bool {
	if m.term == nil || m.term.screen == nil {
		return false
	}
	ox, oy := m.terminalOrigin()
	cols, rows := m.term.screen.Size()
	return x >= ox && x < ox+cols && y >= oy && y < oy+rows
}
