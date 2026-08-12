package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/dedomorozoff/nlsh/internal/config"
	"github.com/dedomorozoff/nlsh/internal/executor"
	"github.com/dedomorozoff/nlsh/internal/feedback"
	"github.com/dedomorozoff/nlsh/internal/prompt"
	"github.com/spf13/cobra"
)

var (
	reset  = "\033[0m"
	bold   = "\033[1m"
	cyan   = "\033[36m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	gray   = "\033[90m"
)

var errCancelQuestion = errors.New("cancelled")

var slashCommands = []string{
	"/exit", "/quit", "/help", "/cd", "/clear", "/pwd", "/history", "/bind", "/mode",
	"/1", "/2", "/3", "/stats", "/retry", "/export", "/alias",
}

type slashCompleter struct {
	cfg *config.Config
}

func autocompletePaths(pathPart string, dirsOnly bool) ([][]rune, int) {
	var searchDir string
	var filePrefix string

	lastSep := strings.LastIndexAny(pathPart, "/\\")
	if lastSep == -1 {
		searchDir = "."
		filePrefix = pathPart
	} else {
		searchDir = pathPart[:lastSep]
		if searchDir == "" {
			searchDir = "/"
		}
		filePrefix = pathPart[lastSep+1:]
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil, 0
	}

	var suggestions [][]rune
	for _, entry := range entries {
		if dirsOnly && !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(filePrefix)) {
			rem := name[len(filePrefix):]
			if entry.IsDir() {
				rem += "/"
			}
			suggestions = append(suggestions, []rune(rem))
		}
	}
	return suggestions, len([]rune(filePrefix))
}

func (c *slashCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	if pos == 0 {
		return nil, 0
	}

	lineStr := string(line[:pos])

	if lineStr[0] == '/' {
		if strings.HasPrefix(lineStr, "/cd ") {
			pathPart := lineStr[len("/cd "):]
			sugs, lenPref := autocompletePaths(pathPart, true)
			return sugs, lenPref
		}

		if strings.HasPrefix(lineStr, "/mode ") {
			modePart := lineStr[len("/mode "):]
			modes := []string{"ai", "help", "shell"}
			var matches [][]rune
			for _, m := range modes {
				if strings.HasPrefix(m, modePart) {
					matches = append(matches, []rune(m[len(modePart):]))
				}
			}
			return matches, len([]rune(modePart))
		}

		// /alias <name>=... — complete alias names
		if strings.HasPrefix(lineStr, "/alias ") && c.cfg != nil {
			aliasInput := lineStr[len("/alias "):]
			if !strings.Contains(aliasInput, "=") {
				var matches [][]rune
				for name := range c.cfg.Aliases {
					if strings.HasPrefix(name, aliasInput) {
						matches = append(matches, []rune(name[len(aliasInput):]))
					}
				}
				return matches, len([]rune(aliasInput))
			}
		}

		if !strings.Contains(lineStr, " ") {
			var matches [][]rune
			for _, cmd := range slashCommands {
				if strings.HasPrefix(cmd, lineStr) {
					matches = append(matches, []rune(cmd[len(lineStr):]))
				}
			}
			return matches, len([]rune(lineStr))
		}
		return nil, 0
	}

	isShellMode := c.cfg != nil && c.cfg.Mode == config.ModeShell
	if lineStr[0] == '!' || isShellMode {
		lastSpace := strings.LastIndexByte(lineStr, ' ')
		var lastWord string
		if lastSpace == -1 {
			if lineStr[0] == '!' {
				lastWord = lineStr[1:]
			} else {
				lastWord = lineStr
			}
		} else {
			lastWord = lineStr[lastSpace+1:]
		}

		sugs, lenPref := autocompletePaths(lastWord, false)
		return sugs, lenPref
	}

	return nil, 0
}

func flushOutput(w io.Writer) {
	if f, ok := w.(*os.File); ok {
		os.Stderr.Sync()
		if f == os.Stdout || f == os.Stderr {
			os.Stdout.Sync()
		}
	}
}

func newReplCmd(rf *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "repl",
		Short: "Interactive mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := newSession(rf.cfg)
			if err != nil {
				return err
			}
			defer s.close()

			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()

		banner := fmt.Sprintf("%s%s.nlsh%s — Natural Language Shell (%srepl%s mode)\n%sType a request or /help for help. Commands: /stats, /retry, /export, /alias, /bind. Use /1, /2, /3 to switch modes.%s\n\n",
			bold, cyan, reset, green, reset, gray, reset)
			fmt.Fprint(out, banner)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			// Try readline, fallback to bufio if not TTY
			if isTTY := isTerminal(in); isTTY {
				return replLoopReadline(ctx, s, rf, out, cmd.ErrOrStderr())
			}
			return replLoop(ctx, s, rf, in, out, cmd.ErrOrStderr())
		},
	}
}

func replLoop(ctx context.Context, s *session, rf *rootFlags, in io.Reader, out, errW io.Writer) error {
	usr, _ := user.Current()
	hostname, _ := os.Hostname()

	isTTY := isTerminal(in)

	s.SetInput(NewBufioReader(in))

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for {
		cwd, _ := os.Getwd()
		promptStr := buildPrompt(usr.Username, hostname, cwd, string(s.cfg.Mode), isTTY)

		fmt.Fprint(out, promptStr)
		fmt.Fprint(out, " ")  // space after prompt
		flushOutput(out)

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(errW, "scanner error: %v\n", err)
				return err
			}
			fmt.Fprintln(out, "\n[EOF - exit]")
			return nil
		}

		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if stop := handleSlash(line, out, s); stop {
				return nil
			}
			continue
		}

		if err := handleTurn(ctx, s, rf, line, out, errW); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			fmt.Fprintf(errW, "%s%s%s\n", red, err, reset)
		}
	}
}

func buildPrompt(username, hostname, cwd, mode string, isTTY bool) string {
	_ = username
	short := shortPath(cwd)
	modeLabel := "ai"
	if mode != "" {
		modeLabel = mode
	}
	modeColor := green
	if modeLabel == "help" {
		modeColor = yellow
	} else if modeLabel == "shell" {
		modeColor = cyan
	}
	return fmt.Sprintf("%s[%s] %s%s%s> %s", gray, short, modeColor, modeLabel, reset, reset)
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

// replLoopReadline — REPL с readline-подобными хоткеями
func replLoopReadline(ctx context.Context, s *session, rf *rootFlags, out, errW io.Writer) error {
	var rl *readline.Instance
	var err error

	usr, _ := user.Current()
	hostname, _ := os.Hostname()

	// History file path
	historyPath := filepath.Join(filepath.Dir(rf.cfg.HistoryFile), "readline_history")
	_ = os.MkdirAll(filepath.Dir(historyPath), 0755)

	// Initialize readline with history
	rlConfig := &readline.Config{
		Prompt:          buildPrompt(usr.Username, hostname, "", string(s.cfg.Mode), false) + " ",
		HistoryFile:     historyPath,
		HistoryLimit:    1000,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    &slashCompleter{cfg: &s.cfg},
	}

	rlConfig.Listener = readline.FuncListener(func(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool) {
		isFirstChar := false
		if key == '/' {
			if len(line) == 0 {
				isFirstChar = true
			} else if len(line) == 1 && line[0] == '/' {
				isFirstChar = true
			}
		}
		if isFirstChar {
			fmt.Fprintf(out, "\n%sCommands:%s\n", cyan, reset)
			fmt.Fprintf(out, "  %s/1%s, %s/mode 1%s  — AI mode (auto-execute)\n", yellow, reset, yellow, reset)
			fmt.Fprintf(out, "  %s/2%s, %s/mode 2%s  — Help mode (command + explanation)\n", yellow, reset, yellow, reset)
			fmt.Fprintf(out, "  %s/3%s, %s/mode 3%s  — Shell mode (direct execution)\n", yellow, reset, yellow, reset)
			fmt.Fprintf(out, "  %s/stats%s        — session statistics\n", yellow, reset)
			fmt.Fprintf(out, "  %s/retry%s        — re-run last request\n", yellow, reset)
			fmt.Fprintf(out, "  %s/export%s       — copy last command to clipboard\n", yellow, reset)
			fmt.Fprintf(out, "  %s/alias%s        — manage aliases\n", yellow, reset)
			fmt.Fprintf(out, "  %s/cd%s <path>    — change directory\n", yellow, reset)
			fmt.Fprintf(out, "  %s/pwd%s          — show current directory\n", yellow, reset)
			fmt.Fprintf(out, "  %s/history%s      — show command history\n", yellow, reset)
			fmt.Fprintf(out, "  %s/clear%s        — clear screen\n", yellow, reset)
			fmt.Fprintf(out, "  %s/mode%s         — show current mode\n", yellow, reset)
			fmt.Fprintf(out, "  %s/bind%s         — show key bindings\n", yellow, reset)
			fmt.Fprintf(out, "  %s/help%s         — show full help\n", yellow, reset)
			fmt.Fprintf(out, "  %s/exit%s         — exit REPL\n", yellow, reset)
			if rl != nil {
				rl.Refresh()
			}
		}
		return nil, 0, false
	})

	// Filter input for special keys
	rlConfig.FuncFilterInputRune = func(r rune) (rune, bool) {
		// Ctrl+L (0x0c) - clear screen
		if r == 0x0c {
			clearScreen(out)
			return 0, false
		}
		return r, true
	}

	rl, err = readline.NewEx(rlConfig)
	if err != nil {
		// Fallback to basic mode if readline fails
		fmt.Fprintf(errW, "%sreadline initialization failed: %v, using basic mode%s\n", yellow, err, reset)
		return replLoop(ctx, s, rf, os.Stdin, out, errW)
	}
	defer rl.Close()

	s.SetInput(NewReadlineReader(rl))

	for {
		cwd, _ := os.Getwd()
		rl.SetPrompt(buildPrompt(usr.Username, hostname, cwd, string(s.cfg.Mode), true) + " ")

		line, err := rl.Readline()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				fmt.Fprintln(out, "\n^C")
				continue
			}
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(out, "\n[EOF - exit]")
				return nil
			}
			fmt.Fprintf(errW, "%sreadline error: %v%s\n", red, err, reset)
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Add to history
		_ = rl.SaveHistory(line)

		if strings.HasPrefix(line, "/") {
			if stop := handleSlash(line, out, s); stop {
				return nil
			}
			continue
		}

		if err := handleTurn(ctx, s, rf, line, out, errW); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, ErrInterrupted) {
				continue
			}
			fmt.Fprintf(errW, "%s%s%s\n", red, err, reset)
		}
	}
}

func handleSlash(line string, out io.Writer, s *session) (stop bool) {
	cfg := &s.cfg
	switch {
	case line == "/exit", line == "/quit", line == "exit", line == "quit":
		fmt.Fprintln(out, "bye!")
		return true
	case line == "/help", line == "help":
		showHelp(out)
	case strings.HasPrefix(line, "/cd "):
		target := strings.TrimPrefix(line, "/cd ")
		target = strings.TrimSpace(target)
		if err := os.Chdir(target); err != nil {
			fmt.Fprintf(out, "%s%s%s\n", red, err, reset)
		}
	case line == "/cd":
		if home, err := os.UserHomeDir(); err == nil {
			if err := os.Chdir(home); err != nil {
				fmt.Fprintf(out, "%s%s%s\n", red, err, reset)
			}
		}
	case line == "/clear", line == "clear":
		clearScreen(out)
	case line == "/pwd", line == "pwd":
		wd, _ := os.Getwd()
		fmt.Fprintln(out, wd)
	case line == "/history", line == "history":
		printHistoryLimit(out, cfg.HistoryFile, 20)
	case line == "/bind", line == "/bind keys":
		showKeyBindings(out)
	case line == "/stats":
		showStats(out, s)
	case line == "/retry":
		// /retry перепросит последний запрос — обрабатывается в handleTurn
		if s.lastInput == "" {
			fmt.Fprintf(out, "%sNo previous request to retry.%s\n", yellow, reset)
			return false
		}
		// возвращаем специальный сигнал — вызывающее место перехватит его через retrySignal
		return false
	case strings.HasPrefix(line, "/export"):
		handleExport(line, out, s)
	case strings.HasPrefix(line, "/alias"):
		handleAlias(line, out, cfg)
	case IsModeCommand(line):
		ms := NewModeSwitcher(cfg, out)
		if line == "/mode" {
			ms.ShowCurrent()
			return false
		}
		newMode := ParseModeCommand(line)
		if newMode != "" {
			ms.Switch(newMode)
		}
	default:
		if strings.HasPrefix(line, "!") {
			cmd := strings.TrimSpace(strings.TrimPrefix(line, "!"))
			fmt.Fprintf(out, "%s$ %s%s\n", cyan, reset, cmd)
			return false
		}
		fmt.Fprintf(out, "%sunknown command: %s%s\n", red, line, reset)
	}
	return false
}

// showStats печатает статистику сессии.
func showStats(out io.Writer, s *session) {
	elapsed := time.Since(s.stats.StartTime).Round(time.Second)
	total := s.stats.CommandsLLM + s.stats.CommandsDirect
	fmt.Fprintf(out, "\n%s%s=== Session Stats ===%s\n", bold, cyan, reset)
	fmt.Fprintf(out, "  %sStarted:%s       %s ago\n", bold, reset, elapsed)
	fmt.Fprintf(out, "  %sRequests:%s      %d\n", bold, reset, s.stats.Requests)
	fmt.Fprintf(out, "  %sCommands run:%s  %d (LLM: %d, direct: %d)\n", bold, reset, total, s.stats.CommandsLLM, s.stats.CommandsDirect)
	fmt.Fprintf(out, "  %sErrors fixed:%s  %d\n", bold, reset, s.stats.ErrorsFix)
	fmt.Fprintf(out, "  %sCurrent mode:%s  %s\n\n", bold, reset, s.cfg.Mode)
}

// handleExport обрабатывает /export.
func handleExport(line string, out io.Writer, s *session) {
	// Берём последнюю команду из recent
	if len(s.recent) == 0 {
		fmt.Fprintf(out, "%sNo commands to export yet.%s\n", yellow, reset)
		return
	}
	lastCmd := s.recent[len(s.recent)-1]

	// /export last > file.sh
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

	// /export [без аргументов] — копировать в буфер обмена
	if err := copyToClipboard(lastCmd); err != nil {
		fmt.Fprintf(out, "%sClipboard unavailable: %v%s\n", yellow, err, reset)
		fmt.Fprintf(out, "%sLast command: %s%s%s\n", gray, cyan, lastCmd, reset)
		return
	}
	fmt.Fprintf(out, "%s✓ Copied to clipboard:%s %s%s%s\n", green, reset, cyan, lastCmd, reset)
}

// handleAlias обрабатывает /alias.
func handleAlias(line string, out io.Writer, cfg *config.Config) {
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/alias"))

	if arg == "" {
		// Показать все алиасы
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

	// /alias -d name — удалить
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

	// /alias name="request" — добавить
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

func showKeyBindings(out io.Writer) {
	fmt.Fprintf(out, "%s%s=== Available Keybindings ===%s\n\n", bold, cyan, reset)
	fmt.Fprintf(out, "%sBasic:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sCtrl+A%s     — beginning of line\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+E%s     — end of line\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+U%s     — delete to beginning of line\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+K%s     — delete to end of line\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+L%s     — clear screen\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+R%s     — reverse history search\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+S%s     — forward history search\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+P%s     — previous command\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+N%s     — next command\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+B%s      — back one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+F%s      — forward one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+D%s      — delete forward one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+W%s     — delete backward one word\n", yellow, reset)
	fmt.Fprintf(out, "\n%sModes (shortcuts):%s\n", bold, reset)
	fmt.Fprintf(out, "  %s/1%s or %s/mode 1%s            — AI mode (auto-execute)\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/2%s or %s/mode 2%s            — Help mode (command + explanation)\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/3%s or %s/mode 3%s            — Shell mode (direct execution)\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "\n%sSpecial:%s\n", bold, reset)
	fmt.Fprintf(out, "  %s/exit%s      — exit REPL\n", yellow, reset)
	fmt.Fprintf(out, "  %s/cd%s path   — change directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/clear%s     — clear screen\n", yellow, reset)
	fmt.Fprintf(out, "  %s/pwd%s       — show current directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/history%s   — show history\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show current mode\n", yellow, reset)
	fmt.Fprintf(out, "  %s/bind keys%s — show this list\n", yellow, reset)
	fmt.Fprintf(out, "  %s!command%s   — execute command directly\n", yellow, reset)
	fmt.Fprintf(out, "\n%sCompletion:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sTab%s        — auto-complete slash commands\n", yellow, reset)
}

func showHelp(out io.Writer) {
	fmt.Fprintf(out, "%s%s=== nlsh help ===%s\n\n", bold, cyan, reset)
	fmt.Fprintf(out, "%sDescription:%s\n  nlsh is a natural language shell. Type \"show files\" and it\n  runs \"ls -la\" for you.\n\n", bold, reset)
	fmt.Fprintf(out, "%sModes:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sAI%s    — AI generates and executes commands automatically (default)\n", yellow, reset)
	fmt.Fprintf(out, "  %sHelp%s  — AI shows command + explanation, you run it manually\n", yellow, reset)
	fmt.Fprintf(out, "  %sShell%s — Direct shell command execution, NL requests via AI\n\n", yellow, reset)
	fmt.Fprintf(out, "%sCommands:%s\n", bold, reset)
	fmt.Fprintf(out, "  plain text    — send request to LLM\n")
	fmt.Fprintf(out, "  %s!command%s   — execute command directly\n", yellow, reset)
	fmt.Fprintf(out, "  %s/cd%s path   — change directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/clear%s     — clear screen\n", yellow, reset)
	fmt.Fprintf(out, "  %s/pwd%s       — show current directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/history%s   — show history\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show current mode\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode ai%s   or %s/mode 1%s or %s/1%s — AI mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/mode help%s or %s/mode 2%s or %s/2%s — Help mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/mode shell%s or %s/mode 3%s or %s/3%s — Shell mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/stats%s     — session statistics\n", yellow, reset)
	fmt.Fprintf(out, "  %s/retry%s     — re-run last request with alternate approach\n", yellow, reset)
	fmt.Fprintf(out, "  %s/export%s    — copy last command to clipboard or /export last > file\n", yellow, reset)
	fmt.Fprintf(out, "  %s/alias%s     — list aliases; /alias name=\"request\" to create; /alias -d name to delete\n", yellow, reset)
	fmt.Fprintf(out, "  %s/bind%s      — show key bindings\n", yellow, reset)
	fmt.Fprintf(out, "  %s/exit%s      — exit\n\n", yellow, reset)
	fmt.Fprintf(out, "%sKeybindings (bash-style):%s\n", bold, reset)
	fmt.Fprintf(out, "  %sCtrl+A%s     — start of line     %sCtrl+E%s     — end of line\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+R%s     — history search    %sCtrl+S%s     — forward search\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+P%s     — previous          %sCtrl+N%s     — next\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+U%s     — delete to start   %sCtrl+K%s     — delete to end\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sAlt+B%s      — back one word     %sAlt+F%s      — forward one word\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+W%s     — delete word back  %sAlt+D%s    — delete word forward\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+L%s     — clear screen      %s/exit%s      — exit\n\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "%sModes (shortcuts):%s\n", bold, reset)
	fmt.Fprintf(out, "  %s/1%s or %s/mode 1%s            — AI mode (auto-execute)\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/2%s or %s/mode 2%s            — Help mode (command + explanation)\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/3%s or %s/mode 3%s            — Shell mode (direct execution)\n\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "%sExamples:%s\n  show all txt files\n  find errors in logs\n  start docker\n\n", bold, reset)
	fmt.Fprintf(out, "%s Default: %sdry-run=false%s (commands execute).\n  Use --dry-run to enable safe mode.\n\n", bold, green, reset)
}

func clearScreen(out io.Writer) {
	fmt.Fprint(out, "\033[H\033[2J\033[3J")
}

func handleTurn(ctx context.Context, s *session, rf *rootFlags, input string, out, errW io.Writer) error {
	input = strings.TrimSpace(input)

	// /retry — повторить последний запрос с альтернативным подходом
	if input == "/retry" {
		if s.lastInput == "" {
			fmt.Fprintf(out, "%sNo previous request to retry.%s\n", yellow, reset)
			return nil
		}
		input = "Alternative approach needed. Previous attempt failed. " + s.lastInput
		fmt.Fprintf(out, "%s[retry]%s %s\n", yellow, reset, s.lastInput)
	}

	// Раскрываем алиасы: если ввод совпадает с именем алиаса — заменяем
	if s.cfg.Aliases != nil {
		if expanded, ok := s.cfg.Aliases[input]; ok {
			fmt.Fprintf(out, "%s[alias]%s %s → %s%s%s\n", gray, reset, input, cyan, expanded, reset)
			input = expanded
		}
	}

	// Direct command execution with ! prefix or in shell mode
	if strings.HasPrefix(input, "!") {
		raw := strings.TrimSpace(strings.TrimPrefix(input, "!"))
		if handled, shouldExit, err := runBuiltin(raw, out, errW, s.recent); handled {
			if err != nil {
				return err
			}
			if shouldExit {
				return context.Canceled
			}
			s.addRecentAndHistory(raw, "direct")
			return nil
		}
		res := executor.RunInteractive(ctx, rf.cfg.Shell, raw)
		s.addRecentAndHistory(raw, "direct")
		if res.Stdout != "" {
			fmt.Fprint(out, res.Stdout)
		}
		if res.Stderr != "" {
			fmt.Fprint(errW, res.Stderr)
		}
		if res.Err != nil {
			return fmt.Errorf("exit %d: %w", res.ExitCode, res.Err)
		}
		return nil
	}

	// Shell mode: try direct execution first, fallback to LLM if it fails or input looks like natural language
	if s.cfg.Mode == config.ModeShell {
		if looksLikeShellCommand(input) {
			if handled, shouldExit, err := runBuiltin(input, out, errW, s.recent); handled {
				if err != nil {
					return err
				}
				if shouldExit {
					return context.Canceled
				}
				s.addRecentAndHistory(input, "direct")
				return nil
			}
			res := executor.RunInteractive(ctx, rf.cfg.Shell, input)
			s.addRecentAndHistory(input, "direct")
			if res.Stdout != "" {
				fmt.Fprint(out, res.Stdout)
			}
			if res.Stderr != "" {
				fmt.Fprint(errW, res.Stderr)
			}
			if res.Err != nil {
				return fmt.Errorf("exit %d: %w", res.ExitCode, res.Err)
			}
			return nil
		}
		// Falls through to LLM if input looks like natural language
	}

	resp, err := askWithFollowUp(ctx, s, "run", input, out, errW)
	if err != nil {
		if errors.Is(err, ErrSlashCommand) {
			return nil
		}
		if errors.Is(err, errCancelQuestion) {
			fmt.Fprintln(out, "(cancelled)")
			return nil
		}
		return err
	}

	if resp.Intent != prompt.IntentRunCommand {
		return nil
	}

	// Help mode: show command + explanation, don't auto-execute
	if s.cfg.Mode == config.ModeHelp {
		fmt.Fprintf(out, "\n%s%s=== Ready Command ===%s%s\n", bold, green, reset, reset)
		fmt.Fprintf(out, "%s$ %s%s%s\n", cyan, reset, resp.Command, reset)
		if resp.Explanation != "" {
			fmt.Fprintf(out, "\n%s%sExplanation:%s %s\n", bold, yellow, reset, resp.Explanation)
		}
		fmt.Fprintf(out, "\n%sCopy the command or prefix with ! to execute immediately%s\n", gray, reset)
		return nil
	}

	// AI mode: auto-execute (with safety checks)
	if rf.cfg.DryRun {
		fmt.Fprintln(out, "(dry-run: command not executed)")
		return nil
	}
	return runCommandWithCorrection(ctx, s, rf, resp, out, errW)
}

// looksLikeShellCommand проверяет, похож ли ввод на shell-команду.
func looksLikeShellCommand(input string) bool {
	if input == "" {
		return false
	}
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false
	}
	cmd := parts[0]
	// Common shell commands and builtins
	commonCmds := map[string]bool{
		"ls": true, "cd": true, "pwd": true, "cat": true, "mkdir": true, "rm": true,
		"cp": true, "mv": true, "chmod": true, "chown": true, "grep": true, "find": true,
		"echo": true, "export": true, "source": true, "alias": true, "which": true, "whoami": true,
		"ps": true, "kill": true, "top": true, "df": true, "du": true, "free": true,
		"tar": true, "zip": true, "unzip": true, "curl": true, "wget": true, "ssh": true,
		"git": true, "docker": true, "npm": true, "pip": true, "python": true, "node": true,
		"go": true, "make": true, "cmake": true, "gcc": true, "g++": true,
		"ipconfig": true, "dir": true, "type": true, "del": true, "copy": true, "move": true,
		"tasklist": true, "taskkill": true, "net": true, "ping": true, "nslookup": true,
	}
	if commonCmds[cmd] {
		return true
	}
	// Check if it starts with ./ or / (likely a path)
	if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "/") || strings.HasPrefix(cmd, "~") {
		return true
	}
	// Windows: check if it has drive letter
	if len(cmd) >= 2 && cmd[1] == ':' && (cmd[0] >= 'A' && cmd[0] <= 'Z' || cmd[0] >= 'a' && cmd[0] <= 'z') {
		return true
	}
	return false
}

// spin — простой спиннер, работающий в горутине, пока не будет остановлен.
type spin struct {
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func startSpin(w io.Writer) *spin {
	s := &spin{stopCh: make(chan struct{})}
	frames := []string{"\U0001f311", "\U0001f312", "\U0001f313", "\U0001f314", "\U0001f315", "\U0001f316", "\U0001f317", "\U0001f318"}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		i := 0
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		fmt.Fprintf(w, "  \U0001f4ad") // initial
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(w, "\r\033[K%s", frames[i%len(frames)])
				flushOutput(w)
				i++
			case <-s.stopCh:
				fmt.Fprintf(w, "\r\033[K")
				flushOutput(w)
				return
			}
		}
	}()
	return s
}

func (s *spin) stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// askWithFollowUp вызывает модель и, если в ответе есть question, задаёт его
// пользователю, передаёт ответ обратно модели и повторяет, пока вопросов больше нет.
func askWithFollowUp(ctx context.Context, s *session, mode, input string, out, errW io.Writer) (prompt.Response, error) {
	for {
		resp, raw, err := s.askStream(ctx, mode, input, out)
		if err != nil {
			if raw != "" {
				fmt.Fprintln(errW, "raw output:")
				fmt.Fprintln(errW, raw)
			}
			return resp, err
		}
		if strings.TrimSpace(resp.Question) == "" {
			return resp, nil
		}

		fmt.Fprintf(out, "%s[nlsh]%s %s%s%s\n", cyan, reset, cyan, resp.Question, reset)

		if s.input == nil {
			return resp, nil
		}
		answer, err := s.input.ReadLine()
		if err != nil {
			return resp, nil
		}
		answer = strings.TrimSpace(answer)

		lower := strings.ToLower(answer)
		if lower == "/exit" || lower == "/cancel" || lower == "exit" || lower == "quit" {
			return prompt.Response{}, errCancelQuestion
		}

		// If user enters a slash command during follow-up, handle it and stop
		if strings.HasPrefix(answer, "/") {
			if stop := handleSlash(answer, out, s); stop {
				return resp, context.Canceled
			}
			// For mode commands and others, stop follow-up loop and return
			return resp, ErrSlashCommand
		}

		input = input + "\n" + answer
	}
}

// runCommandWithCorrection выполняет команду, и в случае ошибки запрашивает автоисправление у LLM.
func runCommandWithCorrection(ctx context.Context, s *session, rf *rootFlags, resp prompt.Response, out, errW io.Writer) error {
	dec := evaluatePolicy(resp, &rf.cfg)
	if !dec.Allowed {
		fmt.Fprintln(out, "(command blocked by security policy)")
		return nil
	}

	if dec.Risk != prompt.RiskLow || resp.NeedsConfirmation {
		ok, err := confirm(s.input, out, s, "execute?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "(cancelled)")
			return nil
		}
	}

	if handled, shouldExit, err := runBuiltin(resp.Command, out, errW, s.recent); handled {
		if err != nil {
			return err
		}
		s.addRecentAndHistory(resp.Command, "llm")
		if shouldExit {
			return context.Canceled
		}
		return nil
	}

	res := executor.Run(ctx, rf.cfg.Shell, resp.Command)
	s.addRecentAndHistory(resp.Command, "llm")

	fb := feedback.Analyze(resp.Command, res.Stdout, res.Stderr, res.ExitCode)
	if res.Stdout != "" {
		fmt.Fprint(out, res.Stdout)
	}

	if fb.Success {
		if hint := fb.Format(); hint != "" {
			fmt.Fprintf(out, "\n%s[nlsh]%s %s%s%s\n", green, reset, green, hint, reset)
		}
		return nil
	}

	// Команда завершилась ошибкой. Запрашиваем исправление.
	stderr := res.Stderr
	if stderr == "" && res.Err != nil {
		stderr = res.Err.Error()
	}

	fmt.Fprintf(out, "\n%s[nlsh]%s Error detected (code %d). Requesting auto-correction from LLM...\n", yellow, reset, res.ExitCode)
	s.stats.ErrorsFix++

	correctionInput := fmt.Sprintf("Command '%s' failed.\nExit code: %d\nStderr:\n%s\n\nPlease fix the command so it runs successfully on the current OS.", resp.Command, res.ExitCode, stderr)

	corrResp, _, err := s.askStream(ctx, "run", correctionInput, out)
	if err != nil {
		return err
	}

	if corrResp.Intent != prompt.IntentRunCommand {
		return nil
	}

	decCorr := evaluatePolicy(corrResp, &rf.cfg)
	if !decCorr.Allowed {
		fmt.Fprintln(out, "(corrected command blocked by security policy)")
		return nil
	}

	ok, err := confirm(s.input, out, s, "execute corrected command?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "(cancelled)")
		return nil
	}

	resCorr := executor.Run(ctx, rf.cfg.Shell, corrResp.Command)
	s.addRecentAndHistory(corrResp.Command, "llm")

	fbCorr := feedback.Analyze(corrResp.Command, resCorr.Stdout, resCorr.Stderr, resCorr.ExitCode)
	if resCorr.Stdout != "" {
		fmt.Fprint(out, resCorr.Stdout)
	}

	if hint := fbCorr.Format(); hint != "" {
		if fbCorr.Success {
			fmt.Fprintf(out, "\n%s[nlsh]%s %s%s%s\n", green, reset, green, hint, reset)
		} else {
			fmt.Fprintf(out, "\n%s[nlsh]%s %s%s%s\n", yellow, reset, yellow, hint, reset)
		}
	}

	if resCorr.Err != nil && !fbCorr.Success {
		return fmt.Errorf("exit %d: %w", resCorr.ExitCode, resCorr.Err)
	}
	return nil
}
