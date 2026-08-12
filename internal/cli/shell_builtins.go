package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runBuiltin выполняет shell-билтины, которые должны менять состояние
// текущего процесса (например, cd), а не дочернего shell.
// Возвращает handled=true, если команда обработана как builtin.
func runBuiltin(raw string, out, errW io.Writer, recent []string) (handled bool, shouldExit bool, err error) {
	cmd := strings.TrimSpace(raw)
	if cmd == "" {
		return false, false, nil
	}
	parts := splitCommand(cmd)
	if len(parts) == 0 {
		return false, false, nil
	}

	switch parts[0] {
	case "cd":
		target := ""
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			home, hErr := os.UserHomeDir()
			if hErr != nil {
				return true, false, fmt.Errorf("cd: %w", hErr)
			}
			target = home
		} else {
			target = expandHome(parts[1])
		}
		target = filepath.Clean(target)
		if err := os.Chdir(target); err != nil {
			return true, false, fmt.Errorf("cd %s: %w", target, err)
		}
		return true, false, nil
	case "pwd":
		wd, err := os.Getwd()
		if err != nil {
			return true, false, err
		}
		fmt.Fprintln(out, wd)
		return true, false, nil
	case "clear":
		// ANSI clear screen
		fmt.Fprint(out, "\033[H\033[2J")
		return true, false, nil
	case "history":
		for i, h := range recent {
			fmt.Fprintf(out, "%4d  %s\n", i+1, h)
		}
		return true, false, nil
	case "exit", "quit":
		return true, true, nil
	case "which":
		if len(parts) < 2 {
			return true, false, fmt.Errorf("which: missing command")
		}
		path, err := exec.LookPath(parts[1])
		if err != nil {
			fmt.Fprintln(errW, err)
			return true, false, nil
		}
		fmt.Fprintln(out, path)
		return true, false, nil
	}
	return false, false, nil
}

func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	sep := "/"
	if runtime.GOOS == "windows" {
		sep = `\`
	}
	if strings.HasPrefix(p, "~"+sep) || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		return filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"), `\`))
	}
	return p
}

// splitCommand — минимальный парсер командной строки для builtin-команд.
// Поддерживает простые кавычки.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
