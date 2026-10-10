// Package executor запускает shell-команды. Обычные команды собирают вывод
// в буфер (Run), а интерактивные идут через псевдотерминал (Terminal) —
// иначе программы, которым нужен настоящий tty, ломаются.
package executor

import (
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// ErrNoTerminal — PTY на этой платформе недоступен. Вызывающий код должен
// откатиться на обычный запуск, а не падать.
var ErrNoTerminal = errors.New("pseudo-terminal is not available on this platform")

// TerminalSize — размер терминала в ячейках.
type TerminalSize struct {
	Cols int
	Rows int
}

// TerminalOpts задаёт окружение дочерней программы.
type TerminalOpts struct {
	Size TerminalSize
	Dir  string
	Env  []string
	// Shell — путь к оболочке. Пусто = системная оболочка пользователя.
	Shell string
}

// TerminalOptsFromShell собирает TerminalOpts под конкретную оболочку.
// Интерактивный запуск отличается от запуска одной строки: у PowerShell
// нужен -NoLogo без -Command, у остальных оболочек — только интерактивный
// флаг.
func TerminalOptsFromShell(shell string, size TerminalSize) TerminalOpts {
	return TerminalOpts{Shell: shell, Size: size}
}

// Run выполняет интерактивный запуск оболочки в псевдотерминале. stdin и
// stdout — терминал пользователя; программа сама решает, что печатать.
// Возвращает код завершения.
func RunTerminal(opts TerminalOpts, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	sess, err := StartTerminal(opts)
	if err != nil {
		return -1, err
	}
	// Ввод не ждём: он блокирован на чтении stdin до конца программы, а
	// закрыть stdin извне нельзя.
	go func() { _, _ = io.Copy(sess, stdin) }()

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, sess)
		close(done)
	}()

	code, err := sess.Wait()
	_ = sess.Close()
	<-done
	return code, err
}

// TerminalSession — оболочка в псевдотерминале, которой управляет вызывающий
// код сам: ввод пишется в Write, вывод читается из Read, терминал
// пользователя при этом не отдаётся. Это то, что нужно встроенному в окно
// режиму терминала.
//
// Close вызывается не больше одного раза: у ConPTY повторный Close
// закрывает уже закрытые win32-хэндлы и ломает кучу процесса. Поэтому
// сессия считает закрытия сама и повторный вызов безопасен.
type TerminalSession struct {
	rd        io.Reader
	wr        io.Writer
	closer    io.Closer
	resizeFn  func(TerminalSize) error
	waitFn    func() (int, error)
	closeOnce sync.Once
	closeErr  error
}

// StartTerminal запускает интерактивную оболочку в псевдотерминале.
func StartTerminal(opts TerminalOpts) (*TerminalSession, error) {
	if strings.TrimSpace(opts.Shell) == "" {
		return nil, ErrNoTerminal
	}
	return startTerminal(opts)
}

// Read читает вывод оболочки. Возвращает io.EOF или ошибку, когда псевдо-
// терминал закрыт — вызывающий код на этом просто останавливает чтение.
func (s *TerminalSession) Read(p []byte) (int, error) { return s.rd.Read(p) }

// Write отправляет ввод оболочке.
func (s *TerminalSession) Write(p []byte) (int, error) { return s.wr.Write(p) }

// Resize меняет размер псевдотерминала: программы, которым он нужен
// ( less, top, редакторы), перерисовываются по этому сигналу.
func (s *TerminalSession) Resize(size TerminalSize) error {
	if s.resizeFn == nil {
		return nil
	}
	if size.Cols < 1 || size.Rows < 1 {
		return nil
	}
	return s.resizeFn(size)
}

// Wait блокируется до выхода оболочки и возвращает её код завершения.
// Повторный вызов возвращает тот же результат: закрывать процесс второй раз
// нельзя.
func (s *TerminalSession) Wait() (int, error) { return s.waitFn() }

// Close завершает псевдотерминал и убивает оболочку. Идемпотентен.
func (s *TerminalSession) Close() error {
	s.closeOnce.Do(func() {
		if s.closer != nil {
			s.closeErr = s.closer.Close()
		}
	})
	return s.closeErr
}

// shellInteractiveArgs возвращает аргументы интерактивного запуска.
func shellInteractiveArgs(shell string) []string {
	low := strings.ToLower(shell)
	switch {
	case strings.Contains(low, "powershell"), strings.Contains(low, "pwsh"):
		return []string{"-NoLogo", "-NoProfile"}
	case strings.HasSuffix(low, "cmd"), strings.HasSuffix(low, "cmd.exe"):
		return nil
	default:
		// bash/zsh/fish: -i включает интерактивный режим, -l читает профиль.
		return []string{"-i", "-l"}
	}
}

// TerminalScript склеивает строки ввода для интерактивной оболочки.
//
// Разделитель строк платформенный: ConPTY отдаёт оболочке строки только с
// CRLF, а с голым "\n" PowerShell не выполняет команду и не выходит на
// "exit" — вызов висит до бесконечности. На unix достаточно "\n".
func TerminalScript(lines ...string) string {
	eol := "\n"
	if runtime.GOOS == "windows" {
		eol = "\r\n"
	}
	return strings.Join(lines, eol) + eol
}

// commandForTerminal собирает exec.Cmd для интерактивного запуска.
func commandForTerminal(opts TerminalOpts) *exec.Cmd {
	cmd := exec.Command(opts.Shell, shellInteractiveArgs(opts.Shell)...)
	cmd.Dir = opts.Dir
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}
	return cmd
}
