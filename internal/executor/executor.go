package executor

import (
	"context"
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

// prepareCommand создаёт exec.Cmd для shell-команды с учётом платформы.
// На Windows + PowerShell добавляет установку UTF-8 кодировки.
func prepareCommand(ctx context.Context, shell, command string) *exec.Cmd {
	args := shellArgs(shell)
	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(args[0]), "powershell") || strings.Contains(strings.ToLower(args[0]), "pwsh") {
			command = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command
		}
	}
	return exec.CommandContext(ctx, args[0], append(args[1:], command)...)
}

// exitCode извлекает код завершения из завершённого процесса.
func exitCode(cmd *exec.Cmd, err error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// RunInteractive исполняет команду в интерактивном режиме с проксированием stdin, stdout, stderr.
func RunInteractive(ctx context.Context, shell, command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{ExitCode: -1, Err: errEmpty}
	}
	cmd := prepareCommand(ctx, shell, command)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	return Result{Err: err, ExitCode: exitCode(cmd, err)}
}

// Run исполняет одну shell-командную строку, проксируя её в системный shell.
func Run(ctx context.Context, shell, command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{ExitCode: -1, Err: errEmpty}
	}
	cmd := prepareCommand(ctx, shell, command)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Err:      err,
		ExitCode: exitCode(cmd, err),
	}
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

