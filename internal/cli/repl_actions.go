package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func clearScreen(out io.Writer) {
	fmt.Fprint(out, "\033[H\033[2J\033[3J")
}

func handleExport(line string, out io.Writer, s *session) {
	if len(s.recent) == 0 {
		fmt.Fprintf(out, "%sNo commands to export yet.%s\n", yellow, reset)
		return
	}
	lastCmd := s.recent[len(s.recent)-1]

	parts := strings.Fields(line)
	if len(parts) >= 3 && parts[1] == "last" && parts[2] == ">" && len(parts) >= 4 {
		filePath := strings.Join(parts[3:], " ")
		if err := os.WriteFile(filePath, []byte(lastCmd+"\n"), 0644); err != nil {
			fmt.Fprintf(out, "%scould not write file: %v%s\n", red, err, reset)
			return
		}
		fmt.Fprintf(out, "%s✓ Written to %s%s\n", green, filePath, reset)
		return
	}

	if err := clipboardWrite(lastCmd); err != nil {
		fmt.Fprintf(out, "%sClipboard unavailable: %v%s\n", yellow, err, reset)
		fmt.Fprintf(out, "%sLast command: %s%s%s\n", gray, cyan, lastCmd, reset)
		return
	}
	fmt.Fprintf(out, "%s✓ Copied to clipboard:%s %s%s%s\n", green, reset, cyan, lastCmd, reset)
}

func handleAlias(line string, out io.Writer, cfg *config.Config) {
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/alias"))

	if arg == "" {
		if len(cfg.Aliases) == 0 {
			fmt.Fprintf(out, "%sNo aliases defined. Use /alias name=\"request\"%s\n", gray, reset)
			return
		}
		fmt.Fprintf(out, "\n%s%s=== Aliases ===%s\n", bold, cyan, reset)
		for k, v := range cfg.Aliases {
			fmt.Fprintf(out, "  %s%s%s = %s%s%s\n", yellow, k, reset, gray, v, reset)
		}
		fmt.Fprintln(out)
		return
	}

	if strings.HasPrefix(arg, "-d ") {
		name := strings.TrimSpace(arg[3:])
		if _, ok := cfg.Aliases[name]; !ok {
			fmt.Fprintf(out, "%salias %q not found%s\n", red, name, reset)
			return
		}
		delete(cfg.Aliases, name)
		_ = config.Save(*cfg)
		fmt.Fprintf(out, "%s✓ Alias %q removed%s\n", green, name, reset)
		return
	}

	eqIdx := strings.IndexByte(arg, '=')
	if eqIdx == -1 {
		fmt.Fprintf(out, "%sUsage: /alias name=\"request\" or /alias -d name%s\n", yellow, reset)
		return
	}
	name := strings.TrimSpace(arg[:eqIdx])
	val := strings.Trim(strings.TrimSpace(arg[eqIdx+1:]), `"`)
	if name == "" || val == "" {
		fmt.Fprintf(out, "%sinvalid alias: name and value must not be empty%s\n", red, reset)
		return
	}
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	cfg.Aliases[name] = val
	_ = config.Save(*cfg)
	fmt.Fprintf(out, "%s✓ Alias %q = %q saved%s\n", green, name, val, reset)
}

func flushOutput(w io.Writer) {
	if f, ok := w.(*os.File); ok {
		_ = f.Sync()
	}
}

func isTerminal(r io.Reader) bool {
	if f, ok := r.(*os.File); ok {
		info, err := f.Stat()
		if err != nil {
			return false
		}
		return (info.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

func shortPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}

	if len(p) > 40 {
		return "..." + p[len(p)-37:]
	}
	return p
}
