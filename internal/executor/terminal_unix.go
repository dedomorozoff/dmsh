//go:build !windows

package executor

import (
	"io"
	"sync"

	"github.com/creack/pty"
)

// runTerminal запускает оболочку в псевдотерминале и проксирует ввод-вывод.
// pty.StartWithSize сам проставит cmd.Stdin/Stdout/Stderr, если они не
// заданы; ввод и вывод от bubbletea приходят как io.Reader/io.Writer, а не
// как *os.File, поэтому проброс делается копированием.
func runTerminal(opts TerminalOpts, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := commandForTerminal(opts)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(opts.Size.Cols),
		Rows: uint16(opts.Size.Rows),
	})
	if err != nil {
		return -1, err
	}

	// Копирование в pty (ввод) не ждём: оно блокировано на чтении stdin
	// до конца программы, а закрыть stdin извне нельзя. Оно завершится
	// вместе с терминалом пользователя.
	go func() { _, _ = io.Copy(ptmx, stdin) }()

	// Вывод читаем в двух местах сразу: отдельные копии для stdout и stderr
	// нужны, потому что stderr отдаётся отдельным writer'ом.
	var out sync.WaitGroup
	out.Add(2)
	go func() { defer out.Done(); _, _ = io.Copy(stdout, ptmx) }()
	go func() { defer out.Done(); _, _ = io.Copy(stderr, ptmx) }()

	waitErr := cmd.Wait()

	// Закрываем pty после выхода программы: читатели получают EOF и
	// освобождают терминал, который к этому моменту уже отдан оболочке.
	_ = ptmx.Close()
	out.Wait()

	return exitCode(cmd, waitErr), nil
}
