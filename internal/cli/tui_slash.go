package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// slashCommands — commands offered by Tab completion and the "/" menu.
// Mode switches come first so they are visible in the truncated menu.
var slashCommands = []string{
	"/mode", "/shell", "/help", "/clear", "/exit", "/quit",
	"/history", "/cd", "/pwd", "/model", "/models", "/setup", "/settings", "/proxy", "/mouse", "/todo", "/stats", "/bind", "/alias", "/export", "/retry", "/search", "/copy",
}

// slashDesc — one-line descriptions for the "/" menu.
var slashDesc = map[string]string{
	"/mode":     "show the modes and pick one",
	"/shell":    "open a shell inside this window",
	"/help":     "show help for commands and modes",
	"/model":    "show the current model",
	"/models":   "model menu: pick, install or switch a model",
	"/setup":    "choose how dmsh connects to the LLM",
	"/settings": "network settings: proxy, endpoint, connection test",
	"/proxy":    "shortcut for /settings",
	"/mouse":    "toggle mouse UI / native text selection and copy",
	"/todo":     "the model's task list: click a task to mark it done",
	"/stats":    "session statistics",
	"/search":   "search the output (Ctrl+F), F3 for the next match",
	"/copy":     "copy the last block of output to the clipboard",
	"/export":   "export the last command",
	"/alias":    "manage aliases",
	"/history":  "recent commands",
	"/cd":       "change directory",
	"/pwd":      "current directory",
	"/clear":    "clear screen",
	"/bind":     "show key bindings",
	"/retry":    "retry the last request",
	"/exit":     "exit",
	"/quit":     "exit",
}

// slashMatches возвращает команды, начинающиеся с prefix (точное совпадение
// исключается: выбирать нечего).
func slashMatches(prefix string) []string {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \t") {
		return nil
	}
	matches := make([]string, 0, len(slashCommands))
	for _, c := range slashCommands {
		if strings.HasPrefix(c, prefix) && c != prefix {
			matches = append(matches, c)
		}
	}
	return matches
}

func (m tuiModel) handleSlash(line string) (tea.Model, tea.Cmd) {
	switch {
	case line == "/exit", line == "/quit", line == "exit", line == "quit":
		m.addLine("bye!")
		return m, tea.Quit
	case line == "/help", line == "help":
		m.showHelp()
	case strings.HasPrefix(line, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(line, "/cd "))
		if err := os.Chdir(target); err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
	case line == "/cd":
		if home, err := os.UserHomeDir(); err == nil {
			if err := os.Chdir(home); err != nil {
				m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
			}
		}
	case line == "/shell", line == "terminal":
		// /shell вне режима терминала всё равно открывает терминал, но
		// режим должен ему соответствовать, иначе следующий ввод снова
		// уйдёт в модель.
		m.rf.cfg.Mode = config.ModeShell
		m.s.cfg.Mode = config.ModeShell
		m.modeLabel = modeLabel(config.ModeShell)
		return m.startTerminal()
	case line == "/clear", line == "clear":
		m.clearContent()
		m.addLine("")
	case line == "/search", line == "/find":
		// Пустой запрос повторяет прошлый поиск: подсветка уже на экране, и
		// удобнее продолжить её, чем начинать с чистого листа.
		return m.startFind(m.findQuery)
	case strings.HasPrefix(line, "/search "), strings.HasPrefix(line, "/find "):
		parts := strings.SplitN(line, " ", 2)
		return m.startFind(strings.TrimSpace(parts[1]))
	case line == "/copy", strings.HasPrefix(line, "/copy "):
		m.handleCopy(line)
	case line == "/pwd", line == "pwd":
		wd, _ := os.Getwd()
		m.addLine(wd)
	case line == "/history", line == "history":
		m.showHistory()
	case line == "/bind", line == "/bind keys":
		m.showKeyBindings()
	case line == "/stats":
		m.showStats()
	case line == "/model":
		m.showModel()
	case line == "/models":
		// То же окно, что Ctrl+O: локальные файлы или каталог провайдера.
		mm, cmd := m.openModelMenu()
		return mm, cmd
	case line == "/settings", line == "/proxy":
		return m.openSettings(), nil
	case line == "/setup":
		return m.openSetup(), nil
	case line == "/todo", line == "todo":
		return m.openTodoMenu(), nil
	case strings.HasPrefix(line, "/todo "):
		handleTodo(strings.TrimSpace(strings.TrimPrefix(line, "/todo ")), io.Discard, m.s)
		m.addLine(fmt.Sprintf("%s=== todo (%d open) ===%s",
			colorBold+colorCyan, todoOpenCount(m), colorReset))
		return m.openTodoMenu(), nil
	case line == "/mouse", strings.HasPrefix(line, "/mouse "):
		want := !m.rf.cfg.Mouse
		if arg := strings.TrimSpace(strings.TrimPrefix(line, "/mouse")); arg != "" {
			switch strings.ToLower(arg) {
			case "on", "true", "1", "yes":
				want = true
			case "off", "false", "0", "no":
				want = false
			default:
				m.addLine(fmt.Sprintf("%susage: /mouse [on|off]%s", colorYellow, colorReset))
				return m, nil
			}
		}
		m.rf.cfg.Mouse = want
		if err := config.Save(m.rf.cfg); err != nil {
			m.addLine(fmt.Sprintf("%sMouse setting applied for this session, but could not save config: %v%s", colorYellow, err, colorReset))
		}
		if want {
			m.addLine("Mouse: on (UI clicks/wheel). To select and copy text, run /mouse off.")
		} else {
			m.addLine("Mouse: off (native terminal text selection/copy enabled). Run /mouse on to restore UI clicks.")
		}
		return m, nil
	case line == "/retry":
		lastInput := m.s.lastRequest()
		if lastInput == "" {
			m.addLine(fmt.Sprintf("%sNo previous request to retry.%s", colorYellow, colorReset))
			return m, nil
		}
		m.clarify = clarification{}
		return m.submitToLLM("Alternative approach needed. Previous attempt failed. " + lastInput)
	case strings.HasPrefix(line, "/export"):
		m.handleExport(line)
	case strings.HasPrefix(line, "/alias"):
		m.handleAlias(line)
	case IsModeCommand(line):
		if line == "/mode" {
			// Пустая строка в /mode ничего не сообщала: открываем список
			// режимов с текущим — выбрать можно и стрелками, и Shift+Tab.
			return m.openModeMenu(), nil
		}
		if newMode := ParseModeCommand(line); newMode != "" {
			mm, cmd := m.switchMode(newMode)
			return mm, cmd
		}
	default:
		m.addLine(fmt.Sprintf("%sunknown command: %s%s", colorRed, line, colorReset))
	}
	return m, nil
}

func (m *tuiModel) showHelp() {
	m.addLine(fmt.Sprintf("%s=== dmsh help ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("%sModes:%s", colorBold, colorReset))
	m.addLine(fmt.Sprintf("  %sai%s      — generate a command and run it automatically", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %shelp%s    — command plus explanation, nothing is executed", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sterminal%s— a shell inside this window; Shift+Tab returns", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sShift+Tab%s — cycle modes, %sCtrl+P%s — pick one in the palette", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/mode%s [ai|help|shell] — show the list of modes or set one", colorYellow, colorReset))
	m.addLine("")
	m.addLine(fmt.Sprintf("%sCommands:%s", colorBold, colorReset))
	m.addLine(fmt.Sprintf("  %s!cmd%s         — run shell command directly", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/cd%s path     — change directory", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/todo%s        — the model's task list; click a task to toggle it", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/pwd%s         — current directory", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/history%s     — recent commands", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/stats%s       — session statistics", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/model%s       — current model", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/models%s      — model menu: files or provider catalog", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/setup%s       — how dmsh connects to the LLM, and how to change it", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/settings%s    - network settings: proxy, endpoint, connection test", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/mouse%s       - toggle mouse UI / native text selection+copy", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/shell%s       - open a shell in this window", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/bind%s        - key bindings", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/help%s        - this info", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/search%s      - search the output (Ctrl+F); F3 moves between matches", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/copy%s        - copy the last output block (or /copy N for N lines)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s/exit%s        - exit (or Ctrl+Q)", colorYellow, colorReset))
}

func (m *tuiModel) showKeyBindings() {
	m.addLine(fmt.Sprintf("%s=== Key Bindings ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("  %sF1%s / %s/help%s    — this info", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+P%s          — command palette (all commands, modes and settings)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+Q%s          — exit", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sEsc%s / %sCtrl+C%s — cancel / stop", colorYellow, colorReset, colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+A/E/U/K%s   — start/end/delete-to-start/delete-to-end", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+R/S%s       — history search", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+F%s         — search the output; Enter keeps the highlight, Esc clears it", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sF3/Shift+F3%s    — next / previous output match", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+N%s          — next history", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sAlt+B/F/D%s      - word back/forward/delete", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sAlt+R/E/S%s      - retry / explain / shell (after a command)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+W%s         - delete word back", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+L%s         - clear screen", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sCtrl+O%s         - model menu (install / switch)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("%sMouse:%s click the folder / mode / model / proxy in the status line, or a row in any window - it does what Enter does",
		colorBold, colorReset))
	m.addLine(fmt.Sprintf("  %s/mouse%s          - toggle off to select and copy text (native terminal selection), on to restore UI clicks", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %swheel%s           - scroll output, and the list in a window", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sscrollbar%s        - click the right edge to jump through the history", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s[ retry ]%s        - buttons under the answer: retry, explain, shell (or Alt+R/E/S)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sTab%s            — complete slash command", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sShift+Tab%s      — cycle modes (ai → help → terminal)", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %s↑/↓%s            — previous / next command from history", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sPgUp/PgDn%s      — scroll output", colorYellow, colorReset))
	m.addLine(fmt.Sprintf("  %sTerminal mode:%s  keys go to the shell; Shift+Tab returns to the prompt", colorYellow, colorReset))
}

// showHistory печатает историю из файла, а не из s.recent: recent хранит
// только последние команды текущей сессии и обрезается до 10 записей, так
// что после перезапуска /history показывал бы почти пустоту.
func (m *tuiModel) showHistory() {
	entries := m.s.historyEntries()
	if len(entries) == 0 {
		m.addLine(fmt.Sprintf("%sno history yet%s", colorGray, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("%s=== history (%d) ===%s", colorBold+colorCyan, len(entries), colorReset))
	for i, e := range entries {
		ts := e.Timestamp.Format("01-02 15:04")
		m.addLine(fmt.Sprintf("%s%4d%s  %s%s%s  %s",
			colorGray, i+1, colorReset,
			colorGray, ts, colorReset,
			e.Command))
	}
}

func (m *tuiModel) showStats() {
	st := m.s.statsSnapshot()
	elapsed := time.Since(st.StartTime).Round(time.Second)
	total := st.CommandsLLM + st.CommandsDirect
	m.addLine(fmt.Sprintf("\n%s=== Session Stats ===%s", colorBold+colorCyan, colorReset))
	m.addLine(fmt.Sprintf("  %sStarted:%s        %s ago", colorBold, colorReset, elapsed))
	m.addLine(fmt.Sprintf("  %sRequests:%s       %d (errors: %d)", colorBold, colorReset, st.Requests, st.RequestErrors))
	m.addLine(fmt.Sprintf("  %sCommands run:%s   %d (LLM: %d, direct: %d)", colorBold, colorReset, total, st.CommandsLLM, st.CommandsDirect))
	m.addLine(fmt.Sprintf("  %sTool steps:%s     %d", colorBold, colorReset, st.ToolSteps))
	m.addLine(fmt.Sprintf("  %sErrors fixed:%s   %d", colorBold, colorReset, st.ErrorsFix))
	if st.TTFTSamples > 0 {
		avgTTFT := st.TotalTTFT / time.Duration(st.TTFTSamples)
		m.addLine(fmt.Sprintf("  %sTTFT:%s           avg %s (last %s)", colorBold, colorReset, avgTTFT.Round(time.Millisecond), st.LastTTFT.Round(time.Millisecond)))
	} else if st.LastTTFT > 0 {
		m.addLine(fmt.Sprintf("  %sTTFT:%s           last %s", colorBold, colorReset, st.LastTTFT.Round(time.Millisecond)))
	}
	if st.LastRequestDur > 0 {
		m.addLine(fmt.Sprintf("  %sLast request:%s   %s", colorBold, colorReset, st.LastRequestDur.Round(time.Millisecond)))
	}
	if st.TotalStreamTime > 0 || st.StreamChars > 0 {
		m.addLine(fmt.Sprintf("  %sStream:%s         %d chunks, %d chars, %s total", colorBold, colorReset, st.StreamChunks, st.StreamChars, st.TotalStreamTime.Round(time.Millisecond)))
	}
	m.addLine(fmt.Sprintf("  %sCurrent mode:%s   %s\n", colorBold, colorReset, modeLabel(m.rf.cfg.Mode)))
}

// showModel печатает экран /model теми же строками, что и REPL: общий
// modelInfoLines, два разных вывода.
func (m *tuiModel) showModel() {
	for _, line := range modelInfoLines(m.s, currentProvider(m.s, m.rf.cfg.Provider), m.localModelPath()) {
		m.addLine(line)
	}
}

// firstNonEmpty возвращает первое непустое значение — для настроек,
// которые могли остаться пустыми в конфиге.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *tuiModel) handleExport(line string) {
	if len(m.s.recent) == 0 {
		m.addLine(fmt.Sprintf("%sNo commands to export yet.%s", colorYellow, colorReset))
		return
	}
	lastCmd := m.s.recent[len(m.s.recent)-1]
	parts := strings.Fields(line)
	if len(parts) >= 3 && parts[1] == "last" && parts[2] == ">" && len(parts) >= 4 {
		filePath := strings.Join(parts[3:], " ")
		if err := os.WriteFile(filePath, []byte(lastCmd+"\n"), 0644); err != nil {
			m.addLine(fmt.Sprintf("%scould not write file: %v%s", colorRed, err, colorReset))
			return
		}
		m.addLine(fmt.Sprintf("%s✓ Written to %s%s", colorGreen, filePath, colorReset))
		return
	}
	if err := clipboardWrite(lastCmd); err != nil {
		m.addLine(fmt.Sprintf("%sClipboard unavailable: %v%s", colorYellow, err, colorReset))
		m.addLine(fmt.Sprintf("%sLast command: %s%s%s", colorGray, colorCyan, lastCmd, colorReset))
		return
	}
	m.addLine(fmt.Sprintf("%s✓ Copied to clipboard:%s %s%s%s", colorGreen, colorReset, colorCyan, lastCmd, colorReset))
}

func (m *tuiModel) handleAlias(line string) {
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/alias"))
	cfg := &m.rf.cfg
	if arg == "" {
		if len(cfg.Aliases) == 0 {
			m.addLine(fmt.Sprintf("%sNo aliases defined. Use /alias name=\"request\"%s", colorGray, colorReset))
			return
		}
		m.addLine(fmt.Sprintf("\n%s=== Aliases ===%s", colorBold+colorCyan, colorReset))
		for k, v := range cfg.Aliases {
			m.addLine(fmt.Sprintf("  %s%s%s = %s%s%s", colorYellow, k, colorReset, colorGray, v, colorReset))
		}
		m.addLine("")
		return
	}
	if strings.HasPrefix(arg, "-d ") {
		name := strings.TrimSpace(arg[3:])
		if _, ok := cfg.Aliases[name]; !ok {
			m.addLine(fmt.Sprintf("%salias %q not found%s", colorRed, name, colorReset))
			return
		}
		delete(cfg.Aliases, name)
		_ = config.Save(*cfg)
		m.addLine(fmt.Sprintf("%s✓ Alias %q removed%s", colorGreen, name, colorReset))
		return
	}
	eqIdx := strings.IndexByte(arg, '=')
	if eqIdx == -1 {
		m.addLine(fmt.Sprintf("%sUsage: /alias name=\"request\" or /alias -d name%s", colorYellow, colorReset))
		return
	}
	name := strings.TrimSpace(arg[:eqIdx])
	val := strings.Trim(strings.TrimSpace(arg[eqIdx+1:]), `"`)
	if name == "" || val == "" {
		m.addLine(fmt.Sprintf("%sinvalid alias: name and value must not be empty%s", colorRed, colorReset))
		return
	}
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	cfg.Aliases[name] = val
	_ = config.Save(*cfg)
	m.addLine(fmt.Sprintf("%s✓ Alias %q = %q saved%s", colorGreen, name, val, colorReset))
}
