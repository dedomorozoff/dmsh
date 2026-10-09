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
	if strings.TrimSpace(opts.Shell) == "" {
		return -1, ErrNoTerminal
	}
	return runTerminal(opts, stdin, stdout, stderr)
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
