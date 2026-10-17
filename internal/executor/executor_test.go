package executor

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testShell возвращает оболочку, доступную в тестовой среде.
func testShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "/bin/sh"
}

// slowEcho печатает первый маркер, ждёт и печатает второй.
func slowEcho(first, second string) string {
	if runtime.GOOS == "windows" {
		return "echo " + first + "; Start-Sleep -Milliseconds 700; echo " + second
	}
	return "echo " + first + "; sleep 1; echo " + second
}

// Главное свойство RunStream: кусок вывода приходит, пока процесс ещё
// работает. Иначе «фоновая» команда снова станет молчаливой до конца.
func TestRunStreamReportsOutputBeforeExit(t *testing.T) {
	chunks := make(chan string, 16)
	results := make(chan Result, 1)
	go func() {
		results <- RunStream(context.Background(), testShell(), slowEcho("stream-first", "stream-second"),
			func(text string, stderr bool) { chunks <- text })
	}()

	select {
	case text := <-chunks:
		if !strings.Contains(text, "stream-first") {
			t.Fatalf("first chunk = %q, want the first line", text)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the first chunk never arrived: the command did not stream")
	}
	// Процесс ещё не завершён — в этом весь смысл.
	select {
	case res := <-results:
		t.Fatalf("the command finished before streaming: stdout=%q", res.Stdout)
	default:
	}

	var res Result
	select {
	case res = <-results:
	case <-time.After(30 * time.Second):
		t.Fatal("the command never finished")
	}
	if !strings.Contains(res.Stdout, "stream-first") || !strings.Contains(res.Stdout, "stream-second") {
		t.Fatalf("the result must keep the whole output: %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
}

// stderr не теряется и помечается: разбор ошибки и автокоррекция смотрят на
// Result, а интерфейс красит stderr отдельно.
func TestRunStreamMarksStderr(t *testing.T) {
	command := "echo to-stderr 1>&2"
	if runtime.GOOS == "windows" {
		command = "[Console]::Error.WriteLine('to-stderr')"
	}
	var gotErr bool
	var text strings.Builder
	res := RunStream(context.Background(), testShell(), command, func(chunk string, stderr bool) {
		if stderr {
			gotErr = true
		}
		text.WriteString(chunk)
	})
	if !strings.Contains(res.Stderr, "to-stderr") {
		t.Fatalf("stderr must stay in the result: %q", res.Stderr)
	}
	if !gotErr {
		t.Fatal("stderr chunks must be reported as stderr")
	}
	if !strings.Contains(text.String(), "to-stderr") {
		t.Fatalf("the callback must see the stderr text: %q", text.String())
	}
}

// Пустая команда не должна ни запускаться, ни звать отдачу вывода.
func TestRunStreamRejectsEmptyCommand(t *testing.T) {
	called := false
	res := RunStream(context.Background(), testShell(), "   ", func(string, bool) { called = true })
	if called {
		t.Fatal("an empty command must not report output")
	}
	if res.Err == nil {
		t.Fatal("an empty command must be an error")
	}
}
