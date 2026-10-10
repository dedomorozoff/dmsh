//go:build windows

package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
)

// ErrConPTYUnavailable — система без ConPTY (нужен Windows 10 1809+).
// Оборачивает ErrNoTerminal, чтобы вызывающий код проверял одну ошибку на
// всех платформах.
var ErrConPTYUnavailable = fmt.Errorf("%w: ConPTY is not available (Windows 10 1809 or newer required)", ErrNoTerminal)

// startTerminal запускает оболочку в псевдо-консоли Windows и отдаёт сессию
// вызывающему коду. В отличие от unix-версии потоки не разделяются: ConPTY
// отдаёт весь вывод программы одним каналом.
func startTerminal(opts TerminalOpts) (*TerminalSession, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, ErrConPTYUnavailable
	}

	cpty, err := conpty.Start(
		commandLine(opts),
		conpty.ConPtyDimensions(max(1, opts.Size.Cols), max(1, opts.Size.Rows)),
		conpty.ConPtyWorkDir(opts.Dir),
		conpty.ConPtyEnv(environ(opts)),
	)
	if err != nil {
		return nil, err
	}

	var waitOnce sync.Once
	var code int
	var waitErr error
	return &TerminalSession{
		rd:     cpty,
		wr:     cpty,
		closer: cpty,
		resizeFn: func(size TerminalSize) error {
			return cpty.Resize(max(1, size.Cols), max(1, size.Rows))
		},
		waitFn: func() (int, error) {
			waitOnce.Do(func() {
				exit, err := cpty.Wait(context.Background())
				if err != nil {
					waitErr, code = err, -1
					return
				}
				code = int(exit)
			})
			return code, waitErr
		},
	}, nil
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