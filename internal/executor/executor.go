package executor

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Result — итог выполнения команды.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

// RunInteractive исполняет команду в интерактивном режиме с проксированием stdin, stdout, stderr.
func RunInteractive(ctx context.Context, shell, command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{ExitCode: -1, Err: errEmpty}
	}
	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(shell), "powershell") || shell == "" {
			command = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command
		}
	}
	args := shellArgs(shell)
	cmd := exec.CommandContext(ctx, args[0], append(args[1:], command)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	res := Result{
		Err:    err,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}
	return res
}

// Run исполняет одну shell-командную строку, проксируя её в системный shell.
func Run(ctx context.Context, shell, command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{ExitCode: -1, Err: errEmpty}
	}
	if runtime.GOOS == "windows" {
		// Добавляем установку UTF-8 кодировки для PowerShell
		if strings.Contains(strings.ToLower(shell), "powershell") || shell == "" {
			command = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command
		}
	}
	args := shellArgs(shell)
	cmd := exec.CommandContext(ctx, args[0], append(args[1:], command)...)

	var stdout, stderr strings.Builder
	cmd.Stdout = io.MultiWriter(&stdout)
	cmd.Stderr = io.MultiWriter(&stderr)

	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}
	return res
}

// shellArgs подбирает интерпретатор и флаг для одиночной команды.
func shellArgs(shell string) []string {
	if shell == "" {
		if runtime.GOOS == "windows" {
			return []string{"powershell", "-NoProfile", "-NoLogo", "-Command"}
		}
		return []string{"/bin/sh", "-c"}
	}
	low := strings.ToLower(shell)
	switch {
	case strings.Contains(low, "powershell"), strings.Contains(low, "pwsh"):
		return []string{shell, "-NoProfile", "-NoLogo", "-Command"}
	case strings.HasSuffix(low, "cmd"), strings.HasSuffix(low, "cmd.exe"):
		return []string{shell, "/C"}
	default:
		return []string{shell, "-c"}
	}
}

type errEmptyCommand struct{}

func (errEmptyCommand) Error() string { return "empty command" }

var errEmpty = errEmptyCommand{}
