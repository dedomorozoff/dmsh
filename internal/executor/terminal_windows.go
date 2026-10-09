//go:build windows

package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/UserExistsError/conpty"
)

// ErrConPTYUnavailable — система без ConPTY (нужен Windows 10 1809+).
// Оборачивает ErrNoTerminal, чтобы вызывающий код проверял одну ошибку на
// всех платформах.
var ErrConPTYUnavailable = fmt.Errorf("%w: ConPTY is not available (Windows 10 1809 or newer required)", ErrNoTerminal)

// runTerminal запускает оболочку в псевдо-консоли Windows. В отличие от
// unix-версии потоки не разделяются: ConPTY отдаёт весь вывод программы
// одним каналом, поэтому stderr получает только диагностику самого dmsh.
func runTerminal(opts TerminalOpts, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if !conpty.IsConPtyAvailable() {
		return -1, ErrConPTYUnavailable
	}

	cpty, err := conpty.Start(
		commandLine(opts),
		conpty.ConPtyDimensions(opts.Size.Cols, opts.Size.Rows),
		conpty.ConPtyWorkDir(opts.Dir),
		conpty.ConPtyEnv(environ(opts)),
	)
	if err != nil {
		return -1, err
	}
	// Close вызывается ровно один раз, ниже: двойной Close закрывает
	// win32-хэндлы повторно и ломает кучу процесса. Поэтому defer здесь
	// только на случай паники до успешного старта копирования.
	closed := false
	defer func() {
		if !closed {
			_ = cpty.Close()
		}
	}()

	// Ввод не ждём: он блокирован на чтении stdin до конца программы, а
	// закрыть stdin извне нельзя.
	go func() { _, _ = io.Copy(cpty, stdin) }()

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, cpty)
		close(done)
	}()

	// Wait возвращает STILL_ACTIVE и ошибку, если контекст отменён; в
	// нашем случае контекст живёт до конца программы.
	exit, waitErr := cpty.Wait(context.Background())

	// После выхода программы чтение из ConPTY не даёт EOF: дескрипторы
	// псевдо-консоли ещё открыты. Закрываем их, иначе копирование вывода
	// не завершится и вызов повиснет.
	_ = cpty.Close()
	closed = true
	<-done

	if waitErr != nil {
		return -1, waitErr
	}
	return int(exit), nil
}

// commandLine собирает строку запуска для CreateProcess: ConPTY принимает
// строку, а не argv.
func commandLine(opts TerminalOpts) string {
	parts := make([]string, 0, 3)
	parts = append(parts, quoteArg(opts.Shell))
	for _, a := range shellInteractiveArgs(opts.Shell) {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// quoteArg экранирует аргумент командной строки Windows.
func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// environ возвращает окружение дочерней программы. ConPTY не наследует
// переменные сам, а TERM нужен программам, которые печатают цвета.
func environ(opts TerminalOpts) []string {
	if len(opts.Env) > 0 {
		return opts.Env
	}
	env := os.Environ()
	if !hasEnv(env, "TERM") {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
