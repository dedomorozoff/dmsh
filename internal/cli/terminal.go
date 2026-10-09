package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/executor"
)

// terminalSession — интерактивная оболочка, запущенная в псевдотерминале.
// Реализует tea.ExecCommand, поэтому tea.Exec отдаёт ей терминал целиком:
// TUI на время работы приостанавливается, а ввод/вывод идут напрямую в
// псевдо-консоль.
type terminalSession struct {
	opts  executor.TerminalOpts
	stdin io.Reader
	out   io.Writer
	errW  io.Writer

	started  bool
	startErr error
	startedA time.Time
	code     int
	runErr   error
}

// SetStdin/SetStdout/SetStderr обязательны для tea.ExecCommand. bubbletea
// отдаёт терминал только когда поле не занято, поэтому оставляем свои.
func (t *terminalSession) SetStdin(r io.Reader) {
	if t.stdin == nil {
		t.stdin = r
	}
}

func (t *terminalSession) SetStdout(w io.Writer) {
	if t.out == nil {
		t.out = w
	}
}

func (t *terminalSession) SetStderr(w io.Writer) {
	if t.errW == nil {
		t.errW = w
	}
}

// Run запускает оболочку и блокируется до её выхода.
func (t *terminalSession) Run() error {
	t.started = true
	t.startedA = time.Now()
	t.code, t.runErr = executor.RunTerminal(t.opts, t.stdin, t.out, t.errW)
	if t.runErr != nil {
		t.runErr = fmt.Errorf("start %s: %w", t.opts.Shell, t.runErr)
	}
	return t.runErr
}

// shellCommand собирает команду для терминала: интерактивная оболочка
// текущей сессии плюс рабочий каталог и размер окна.
func (m tuiModel) shellCommand() *terminalSession {
	cwd, _ := os.Getwd()
	size := executor.TerminalSize{Cols: m.width, Rows: m.height}
	if size.Cols <= 0 {
		size.Cols = 80
	}
	if size.Rows <= 0 {
		size.Rows = 24
	}
	opts := executor.TerminalOptsFromShell(m.rf.cfg.Shell, size)
	opts.Dir = cwd
	return &terminalSession{opts: opts}
}

// openTerminal уходит в реальную оболочку. tea.Exec освобождает терминал,
// запускает сессию и возвращает управление TUI после её завершения.
func (m tuiModel) openTerminal() (tea.Model, tea.Cmd) {
	shell := m.rf.cfg.Shell
	if shell == "" {
		m.addLine(fmt.Sprintf("%sno shell configured%s", colorRed, colorReset))
		return m, nil
	}
	term := m.shellCommand()
	return m, tea.Exec(term, func(err error) tea.Msg {
		return shellExitedMsg{code: term.code, err: errOrRunErr(err, term.runErr), elapsed: term.startedA}
	})
}

// shellExitedMsg — оболочка завершилась, TUI забирает терминал обратно.
type shellExitedMsg struct {
	code    int
	err     error
	elapsed time.Time
}

// errOrRunErr отдаёт ошибку из tea.Exec, а если её нет — из запуска:
// tea.Exec оборачивает ошибку Run, но в разных версиях по-разному.
func errOrRunErr(execErr, runErr error) error {
	if runErr != nil {
		return runErr
	}
	return execErr
}

// startTerminalCommand выполняет одну команду в псевдотерминале, не отдавая
// терминал TUI. Команда уходит в оболочку через stdin, поэтому получает
// настоящий tty: цвета, интерактивные программы и размер окна работают.
//
// Приглашение оболочки и управляющие последовательности в выводе не нужны —
// они попали бы в транскрипт мусором, поэтому вывод прогоняется через
// ansi.Strip и обрезается по маркерам приглашения.
func (m tuiModel) startTerminalCommand(command string) (tea.Model, tea.Cmd) {
	term := m.shellCommand()
	return m, func() tea.Msg {
		var raw bytes.Buffer
		// exit закрывает оболочку после выполнения команды: иначе pty ждёт
		// вечно. Разделитель строк платформенный, см. TerminalScript.
		script := executor.TerminalScript(command, "exit")
		code, err := executor.RunTerminal(term.opts, strings.NewReader(script), &raw, &raw)
		return terminalRanMsg{
			command: command,
			output:  stripShellNoise(ansi.Strip(raw.String()), command),
			code:    code,
			err:     err,
		}
	}
}

// stripShellNoise убирает из вывода оболочки то, что не является выводом
// команды: баннер, приглашения и эхо введённых строк. Escape-последовательности
// к этому моменту уже сняты вызывающим кодом.
//
// Эхо опознаётся по совпадению с заданной командой на конце строки: оболочка
// печатает «<приглашение> <команда>», поэтому сравнение с суффиксом не
// зависит от вида приглашения («PS D:\dmsh>», «user@host:~$»). Приглашение без
// ввода распознаётся по «>» или «$» в конце.
func stripShellNoise(out, command string) string {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")

	start := -1
	for i, line := range lines {
		if endsWithCommand(line, command) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		// Оболочка не отэхоила команду: выводом считается всё, кроме строк
		// с чистым приглашением.
		return joinNonPrompt(lines)
	}
	// Хвост после вывода — это приглашение и эхо «exit».
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if isPromptOnly(lines[i]) || endsWithCommand(lines[i], "exit") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// endsWithCommand сообщает, что строка оканчивается заданной командой —
// так выглядит эхо ввода оболочкой.
func endsWithCommand(line, command string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == command || strings.HasSuffix(trimmed, " "+command)
}

// isPromptOnly сообщает, что строка состоит только из приглашения оболочки.
func isPromptOnly(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	last := trimmed[len(trimmed)-1]
	return last == '>' || last == '$'
}

// joinNonPrompt склеивает строки, выбрасывая чистые приглашения и хвостовые
// пустые строки от завершения оболочки.
func joinNonPrompt(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isPromptOnly(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

// terminalRanMsg — однострочная команда в терминале завершилась.
type terminalRanMsg struct {
	command string
	output  string
	code    int
	err     error
}

// printTerminalRun выводит результат однострочной команды в транскрипт и
// пишет запись в историю/аудит тем же путём, что и остальные команды.
func (m *tuiModel) printTerminalRun(msg terminalRanMsg) {
	m.state = tuiIdle
	switch {
	case msg.err != nil:
		m.addLine(fmt.Sprintf("%sterminal: %v%s", colorRed, msg.err, colorReset))
	case msg.code != 0:
		m.addLine(fmt.Sprintf("%sexit %d%s", colorYellow, msg.code, colorReset))
	}
	if msg.output != "" {
		m.addLine(msg.output)
	}
	dec := directDecision()
	m.s.addRecentAndHistory(msg.command, "direct")
	m.s.audit(msg.command, "direct", dec, executor.Result{ExitCode: msg.code, Err: msg.err})
}

// resumeFromShell печатает результат терминала и возвращает TUI в idle.
func (m *tuiModel) resumeFromShell(msg shellExitedMsg) {
	m.state = tuiIdle
	switch {
	case msg.err != nil:
		m.addLine(fmt.Sprintf("%sterminal: %v%s", colorRed, msg.err, colorReset))
	case msg.code != 0:
		m.addLine(fmt.Sprintf("%s[shell exited with code %d]%s", colorYellow, msg.code, colorReset))
	default:
		m.addLine(fmt.Sprintf("%s[shell exited]%s", colorGray, colorReset))
	}
	// Выход из оболочки считаем командой в истории: пользователь наверняка
	// что-то делал, и в /history это должно быть видно.
	m.s.addRecentAndHistory(m.rf.cfg.Shell+" (interactive)", "direct")
}
