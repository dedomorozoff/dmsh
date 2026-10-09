package executor

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

// interactiveShell возвращает оболочку, доступную в тестовой среде.
func interactiveShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "/bin/sh"
}

// exitScript — ввод, который заставляет интерактивную оболочку завершиться.
func exitScript() string {
	return TerminalScript("exit")
}

// Однострочная команда с последующим выходом. Именно этот путь использует
// терминальный режим TUI, и именно он зависал с "\n" на Windows: ConPTY
// отдаёт оболочке строки только с CRLF.
func commandScript(command string) string {
	return TerminalScript(command, "exit")
}

func TestShellInteractiveArgs(t *testing.T) {
	if got := shellInteractiveArgs("powershell"); len(got) == 0 || got[0] == "-Command" {
		t.Fatalf("powershell must not run with -Command: %v", got)
	}
	if got := shellInteractiveArgs("/bin/bash"); len(got) < 1 || got[0] != "-i" {
		t.Fatalf("bash must run interactively: %v", got)
	}
	if got := shellInteractiveArgs("cmd.exe"); len(got) != 0 {
		t.Fatalf("cmd.exe needs no flags: %v", got)
	}
}

func TestRunTerminalNeedsShell(t *testing.T) {
	if _, err := RunTerminal(TerminalOpts{}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("empty shell must be rejected, not panic")
	}
}

// Регрессия: терминальный режим TUI гоняет однострочные команды через
// интерактивную оболочку. На Windows разделитель строк обязан быть CRLF —
// с голым "\n" PowerShell не выполняет команду и не выходит на "exit",
// вызов висит бесконечно.
func TestTerminalScriptUsesPlatformEOL(t *testing.T) {
	got := TerminalScript("echo hi", "exit")
	want := "echo hi\nexit\n"
	if runtime.GOOS == "windows" {
		want = "echo hi\r\nexit\r\n"
	}
	if got != want {
		t.Fatalf("TerminalScript = %q, want %q", got, want)
	}
}

func TestRunTerminalRunsSingleCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns an interactive shell")
	}
	opts := TerminalOptsFromShell(interactiveShell(), TerminalSize{Cols: 80, Rows: 24})

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := RunTerminal(opts, strings.NewReader(commandScript("echo dmsh-single-cmd")), &out, &out)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			if errors.Is(err, ErrNoTerminal) {
				t.Skipf("no pseudo-terminal on this platform: %v", err)
			}
			t.Fatalf("RunTerminal: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("RunTerminal hung: the shell never reached the exit command")
	}
	if !strings.Contains(out.String(), "dmsh-single-cmd") {
		t.Fatalf("command output missing, got %q", out.String())
	}
}

// Терминал должен дать программе настоящий tty: иначе всё, что печатает
// интерактивная оболочка, теряется. Проверяем вывод и код завершения.
func TestRunTerminalExitsAndCapturesOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns an interactive shell")
	}
	opts := TerminalOptsFromShell(interactiveShell(), TerminalSize{Cols: 80, Rows: 24})
	if opts.Size.Cols != 80 || opts.Size.Rows != 24 {
		t.Fatalf("size = %+v, want 80x24", opts.Size)
	}

	var out bytes.Buffer
	done := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := RunTerminal(opts, strings.NewReader(exitScript()), &out, io.Discard)
		done <- struct {
			code int
			err  error
		}{code, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			if errors.Is(got.err, ErrNoTerminal) {
				t.Skipf("no pseudo-terminal on this platform: %v", got.err)
			}
			t.Fatalf("RunTerminal: %v", got.err)
		}
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0", got.code)
		}
		if out.Len() == 0 {
			t.Fatal("interactive shell produced no output: the pty is not wired to stdout")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("RunTerminal did not return: the shell was not driven to exit")
	}
}
