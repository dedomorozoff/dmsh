package feedback

import (
	"fmt"
	"strings"
)

// Result represents a command result with a human-readable hint.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Success  bool
	Hint     string
}

// Analyze turns common command output into a short explanation. Detection
// accepts both English and Russian stderr text (Windows locales vary), but
// all user-facing text is English, like the rest of the interface.
func Analyze(command, stdout, stderr string, exitCode int) Result {
	success := exitCode == 0 && stderr == ""

	if !success {
		return Result{
			ExitCode: exitCode,
			Stdout:   stdout,
			Stderr:   stderr,
			Success:  false,
			Hint:     analyzeError(command, stderr, exitCode),
		}
	}

	return Result{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Success:  true,
		Hint:     analyzeSuccess(command, stdout),
	}
}

func analyzeError(command, stderr string, exitCode int) string {
	lower := strings.ToLower(stderr)
	_ = command // reserved for command-specific hints

	if strings.Contains(lower, "уже существует") || strings.Contains(lower, "already exists") {
		return "File or folder already exists. Use a different name or force an overwrite."
	}
	if strings.Contains(lower, "не найден") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "cannot find") || strings.Contains(lower, "does not exist") {
		return "File or folder not found. Check the path or name."
	}
	if strings.Contains(lower, "доступ запрещен") || strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "permission denied") {
		return "Permission denied. Try running with elevated privileges."
	}
	if strings.Contains(lower, "используется другим процессом") || strings.Contains(lower, "being used by another process") {
		return "File is locked by another process. Close the program that uses it."
	}
	if strings.Contains(lower, "нет места") || strings.Contains(lower, "disk full") ||
		strings.Contains(lower, "not enough space") {
		return "Not enough disk space. Free some space and try again."
	}
	if strings.Contains(lower, "syntax") || strings.Contains(lower, "синтаксис") {
		return "Command syntax error. Check the spelling and arguments."
	}
	if strings.Contains(lower, "не распознано") || strings.Contains(lower, "not recognized") ||
		strings.Contains(lower, "command not found") {
		return "Command not found. Install the program or check PATH."
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return "The operation timed out. Try again or check the network."
	}
	if strings.Contains(lower, "network") || strings.Contains(lower, "сеть") ||
		strings.Contains(lower, "connection refused") {
		return "Network problem. Check your connection."
	}

	if strings.Contains(stderr, "FullyQualifiedErrorId") {
		if strings.Contains(stderr, "NewItemIOError") {
			return "Could not create the item. The path may be invalid or the file already exists."
		}
		if strings.Contains(stderr, "PathNotFound") {
			return "Path not found. Check that the directory exists."
		}
	}

	if exitCode != 0 {
		return fmt.Sprintf("Command exited with code %d. Check the arguments.", exitCode)
	}
	return ""
}

func analyzeSuccess(command, stdout string) string {
	cmdLower := strings.ToLower(command)

	if strings.Contains(cmdLower, "new-item") || strings.Contains(cmdLower, "mkdir") ||
		strings.Contains(cmdLower, "touch") {
		if strings.Contains(stdout, "Directory:") || strings.Contains(stdout, "Mode") {
			return "Done!"
		}
	}
	if strings.Contains(cmdLower, "remove-item") || strings.Contains(cmdLower, "rm ") ||
		strings.Contains(cmdLower, "rmdir") || strings.Contains(cmdLower, "del ") {
		return "Removed successfully."
	}
	if strings.Contains(cmdLower, "copy-item") || strings.Contains(cmdLower, "cp ") {
		return "Copied successfully."
	}
	if strings.Contains(cmdLower, "move-item") || strings.Contains(cmdLower, "mv ") {
		return "Moved successfully."
	}
	if strings.Contains(cmdLower, "get-childitem") || strings.Contains(cmdLower, "dir") ||
		strings.Contains(cmdLower, "ls") {
		if strings.Count(stdout, "\n") == 0 && strings.TrimSpace(stdout) == "" {
			return "The folder is empty."
		}
	}
	return ""
}

// Format returns the hint in a display-ready form.
func (r Result) Format() string { return r.Hint }
