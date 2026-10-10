//go:build !windows

package executor

import (
	"sync"

	"github.com/creack/pty"
)

// startTerminal запускает оболочку в псевдотерминале и отдаёт сессию
// вызывающему коду: master-конец и есть одновременно ввод и вывод.
func startTerminal(opts TerminalOpts) (*TerminalSession, error) {
	cmd := commandForTerminal(opts)

	ptmx, err := pty.StartWithSize(cmd, winsize(opts.Size))
	if err != nil {
		return nil, err
	}

	var waitOnce sync.Once
	var code int
	var waitErr error
	return &TerminalSession{
		rd:     ptmx,
		wr:     ptmx,
		closer: ptmx,
		resizeFn: func(size TerminalSize) error {
			return pty.Setsize(ptmx, winsize(size))
		},
		waitFn: func() (int, error) {
			waitOnce.Do(func() {
				waitErr = cmd.Wait()
				code = exitCode(cmd, waitErr)
			})
			return code, waitErr
		},
	}, nil
}

// winsize переводит размер в ячейках в формат pty. Нулевые значения
// заменяются на минимум: нулевой размер псевдотерминал не принимает.
func winsize(size TerminalSize) *pty.Winsize {
	cols, rows := size.Cols, size.Rows
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
}