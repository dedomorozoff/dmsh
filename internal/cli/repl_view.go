package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/redact"
)

// showTodo печатает список задач, который ведёт модель через tool todo.
func showTodo(out io.Writer, s *session) {
	if s.todos == nil {
		fmt.Fprintf(out, "%stodo list is not available.%s\n", yellow, reset)
		return
	}
	items := s.todos.Items()
	if len(items) == 0 {
		fmt.Fprintf(out, "%stodo list is empty.%s\n", gray, reset)
		return
	}
	fmt.Fprintf(out, "\n%s%s=== todo (%d open) ===%s\n", bold, cyan, s.todos.Open(), reset)
	for _, it := range items {
		if it.Done {
			fmt.Fprintf(out, "  %s[%d] %s %s(done)%s\n", gray, it.ID, it.Task, gray, reset)
			continue
		}
		fmt.Fprintf(out, "  %s[%d]%s %s\n", colorYellow, it.ID, reset, it.Task)
	}
	fmt.Fprintln(out)
}

// handleTodo поддерживает `/todo clear` — всё остальное отдаёт модели.
func handleTodo(arg string, out io.Writer, s *session) {
	switch strings.ToLower(arg) {
	case "":
		showTodo(out, s)
	case "clear", "reset", "rm":
		if s.todos == nil {
			return
		}
		n := s.todos.Clear()
		fmt.Fprintf(out, "%stodo list cleared (%d item(s))%s\n", gray, n, reset)
	default:
		fmt.Fprintf(out, "%susage: /todo | /todo clear%s\n", gray, reset)
	}
}

func showStats(out io.Writer, s *session) {
	st := s.statsSnapshot()
	elapsed := time.Since(st.StartTime).Round(time.Second)
	total := st.CommandsLLM + st.CommandsDirect
	fmt.Fprintf(out, "\n%s%s=== Session Stats ===%s\n", bold, cyan, reset)
	fmt.Fprintf(out, "  %sStarted:%s        %s ago\n", bold, reset, elapsed)
	fmt.Fprintf(out, "  %sRequests:%s       %d (errors: %d)\n", bold, reset, st.Requests, st.RequestErrors)
	fmt.Fprintf(out, "  %sCommands run:%s   %d (LLM: %d, direct: %d)\n", bold, reset, total, st.CommandsLLM, st.CommandsDirect)
	fmt.Fprintf(out, "  %sTool steps:%s     %d\n", bold, reset, st.ToolSteps)
	fmt.Fprintf(out, "  %sErrors fixed:%s   %d\n", bold, reset, st.ErrorsFix)
	if st.TTFTSamples > 0 {
		avgTTFT := st.TotalTTFT / time.Duration(st.TTFTSamples)
		fmt.Fprintf(out, "  %sTTFT:%s           avg %s (last %s)\n", bold, reset, avgTTFT.Round(time.Millisecond), st.LastTTFT.Round(time.Millisecond))
	} else if st.LastTTFT > 0 {
		fmt.Fprintf(out, "  %sTTFT:%s           last %s\n", bold, reset, st.LastTTFT.Round(time.Millisecond))
	}
	if st.LastRequestDur > 0 {
		fmt.Fprintf(out, "  %sLast request:%s   %s\n", bold, reset, st.LastRequestDur.Round(time.Millisecond))
	}
	if st.TotalStreamTime > 0 || st.StreamChars > 0 {
		fmt.Fprintf(out, "  %sStream:%s         %d chunks, %d chars, %s total\n", bold, reset, st.StreamChunks, st.StreamChars, st.TotalStreamTime.Round(time.Millisecond))
	}
	fmt.Fprintf(out, "  %sCurrent mode:%s   %s\n\n", bold, reset, s.cfg.Mode)
}

// showModel печатает экран /model теми же строками, что и TUI: общий
// modelInfoLines, два разных вывода.
func showModel(out io.Writer, s *session) {
	provider := currentProvider(s, "")
	modelPath := firstNonEmpty(s.cfg.ModelPath, s.cfg.DefaultModel)
	for _, line := range modelInfoLines(s, provider, modelPath) {
		fmt.Fprintln(out, line)
	}
}

// modelInfoLines собирает строки экрана /model — общий текст для TUI и REPL,
// чтобы два вывода не разъезжались. Провайдер и путь к модели выбираются
// снаружи: у TUI есть ещё конфиг оболочки, у REPL — DefaultModel.
func modelInfoLines(s *session, provider config.Provider, modelPath string) []string {
	lines := []string{
		"",
		fmt.Sprintf("%s%s=== Model ===%s", bold, cyan, reset),
		fmt.Sprintf("  %sProvider:%s  %s", bold, reset, provider),
	}
	if showsRemoteProvider(provider, modelPath) {
		lines = append(lines,
			fmt.Sprintf("  %sIn use:%s  %s", bold, reset,
				firstNonEmpty(s.cfg.RemoteModel, config.DefaultModelFor(provider))),
			fmt.Sprintf("  %sEndpoint:%s %s", gray, reset, remoteEndpoint(provider, s.cfg.RemoteBaseURL)),
		)
		if provider == config.ProviderPollinations {
			lines = append(lines, fmt.Sprintf("  %sSearch:%s   %s", bold, reset,
				firstNonEmpty(s.cfg.SearchModel, config.DefaultSearchModel)))
		}
		lines = append(lines,
			fmt.Sprintf("  %sTools:%s    %s", bold, reset, toolsLabel(s.cfg.ToolsEnabled)),
			// Ключ не печатается: он виден только как маска, и то в setup.
			fmt.Sprintf("  %sAuth:%s     %s", gray, reset, apiKeyLabel(provider, s.cfg)),
			"",
		)
		return lines
	}
	if modelPath == "" {
		return append(lines, fmt.Sprintf("  %snone%s", yellow, reset), "")
	}
	lines = append(lines, fmt.Sprintf("  %sIn use:%s  %s", bold, reset, modelPath))
	if fi, err := os.Stat(modelPath); err == nil {
		lines = append(lines, fmt.Sprintf("  %sSize:%s    %d MB", bold, reset, fi.Size()/1024/1024))
	}
	if provider == config.ProviderAuto {
		lines = append(lines, fmt.Sprintf("  %sHint:%s    dmsh config set provider ollama — локальный сервер вместо GGUF", gray, reset))
	}
	return append(lines, "")
}

// showModels печатает список моделей вне TUI: каталог провайдера или
// локальные .gguf. В TUI то же окно открывается по /models и Ctrl+O.
func showModels(out io.Writer, s *session) {
	provider := currentProvider(s, "")
	modelPath := firstNonEmpty(s.cfg.ModelPath, s.cfg.DefaultModel)
	if !showsRemoteProvider(provider, modelPath) {
		listLocalModels(out)
		return
	}
	if provider == config.ProviderAuto {
		// auto без локального GGUF работает через Pollinations — тот же
		// выбор, что делает resolveProvider при старте.
		provider = config.ProviderPollinations
	}
	fmt.Fprintf(out, "\n%s%s=== models: %s catalog ===%s\n", bold, cyan, provider, reset)
	// Ключ, как и в меню, не подставляется: каталог открыт без него.
	models, err := llm.ListRemoteModels(llm.Params{
		Provider:      toLLMProvider(provider),
		RemoteModel:   s.cfg.RemoteModel,
		RemoteBaseURL: s.cfg.RemoteBaseURL,
		APIKey:        s.cfg.APIKey(),
		Proxy:         s.cfg.Proxy(),
	})
	if err != nil {
		fmt.Fprintf(out, "%s%s%s\n", red, err, reset)
		return
	}
	if len(models) == 0 {
		fmt.Fprintf(out, "%sthe provider reported no models%s\n", gray, reset)
		return
	}
	for _, rm := range models {
		if rm.Note != "" {
			fmt.Fprintf(out, "  %s  %s(%s)%s\n", rm.ID, gray, rm.Note, reset)
			continue
		}
		fmt.Fprintf(out, "  %s\n", rm.ID)
	}
	fmt.Fprintln(out)
}

// showsRemoteProvider решает, показывать ли удалённую часть /model. Провайдер
// auto после resolveProvider уже заменён на конкретный, но в конфиге он
// остаётся: «auto» значит «локальная модель, иначе удалённая», а не
// «удалённая».
func showsRemoteProvider(provider config.Provider, modelPath string) bool {
	switch provider {
	case config.ProviderLocal:
		return false
	case config.ProviderAuto:
		return strings.TrimSpace(modelPath) == ""
	default:
		return true
	}
}

// remoteEndpoint подставляет адрес провайдера, если он не задан. Для
// локального GGUF адреса нет — там показывается адрес удалённого по
// умолчанию: настройки прокси и endpoint всё равно относятся к сетевым
// запросам, а не к локальной модели.
func remoteEndpoint(provider config.Provider, base string) string {
	if s := strings.TrimSpace(base); s != "" {
		return redact.URL(s)
	}
	if provider == config.ProviderOllama {
		return llm.DefaultOllamaBaseURL
	}
	return llm.DefaultPollinationsBaseURL
}

func toolsLabel(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// apiKeyLabel описывает ключ без его содержимого: видно только, откуда он
// взят. Значение секрета в вывод сессии попадать не должно.
func apiKeyLabel(provider config.Provider, cfg config.Config) string {
	if provider == config.ProviderOllama {
		return "not needed (local server)"
	}
	switch cfg.APIKeySource() {
	case "env":
		return "api key from " + config.APIKeyEnv
	case "config":
		return "api key from config.json"
	default:
		return "no api key"
	}
}

func showHelp(out io.Writer) {
	fmt.Fprintf(out, "%s%s=== dmsh help ===%s\n\n", bold, cyan, reset)
	fmt.Fprintf(out, "%sDescription:%s\n  dmsh is a natural language shell. Type \"show files\" and it\n  runs \"ls -la\" for you.\n\n", bold, reset)
	fmt.Fprintf(out, "%sModes:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sAI%s      — AI generates commands; dry-run is on by default\n", yellow, reset)
	fmt.Fprintf(out, "  %sHelp%s    — command + explanation, nothing is executed\n", yellow, reset)
	fmt.Fprintf(out, "  %sTerminal%s — shell session in a pseudo-terminal inside the app window\n\n", yellow, reset)

	fmt.Fprintf(out, "  switch with %sShift+Tab%s (cycle), %sCtrl+P%s (palette) or %s/mode <name>%s\n\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "%sCommands:%s\n", bold, reset)
	fmt.Fprintf(out, "  plain text    — send request to LLM\n")
	fmt.Fprintf(out, "  %s!command%s   — execute command directly\n", yellow, reset)
	fmt.Fprintf(out, "  %s/cd%s path   — change directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/clear%s     — clear screen\n", yellow, reset)
	fmt.Fprintf(out, "  %s/pwd%s       — show current directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/history%s   — show history\n", yellow, reset)
	fmt.Fprintf(out, "  %s/audit%s     — show audit log of executed commands\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show the modes and the current one\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode ai%s   — AI mode (execution still needs dry-run=false)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode help%s — explain-only mode (run_command disabled)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode shell%s — terminal mode\n", yellow, reset)
	fmt.Fprintf(out, "  %s/stats%s     — session statistics\n", yellow, reset)
	fmt.Fprintf(out, "  %s/todo%s      — show the task list kept by the model\n", yellow, reset)
	fmt.Fprintf(out, "  %s/plan%s      — /plan <request>, then /plan run to execute the steps\n", yellow, reset)
	fmt.Fprintf(out, "  %s/model%s     — show the provider and model currently in use\n", yellow, reset)
	fmt.Fprintf(out, "  %s/models%s    — list local models or the provider's catalog\n", yellow, reset)
	fmt.Fprintf(out, "  %s/setup%s     — show how dmsh connects to the LLM and how to change it\n", yellow, reset)
	fmt.Fprintf(out, "  %s/settings%s  - show network settings (proxy, endpoint) and how to change them\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mouse%s     - toggle mouse UI / native text selection+copy (TUI)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/export%s    — copy last command to clipboard or /export last > file\n", yellow, reset)
	fmt.Fprintf(out, "  %s/alias%s     — list aliases; /alias name=\"request\" to create; /alias -d name to delete\n", yellow, reset)
	fmt.Fprintf(out, "  %s/bind%s      — show key bindings\n", yellow, reset)
	fmt.Fprintf(out, "  %s/exit%s      — exit\n\n", yellow, reset)
	fmt.Fprintf(out, "%sKeybindings (bash-style):%s\n", bold, reset)
	fmt.Fprintf(out, "  %sCtrl+A%s     — start of line     %sCtrl+E%s     — end of line\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+R%s     — history search    %sCtrl+S%s     — forward search\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+N%s     — next history      %sCtrl+P%s     — command palette\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+U%s     — delete to start   %sCtrl+K%s     — delete to end\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sAlt+B%s      — back one word     %sAlt+F%s      — forward one word\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+W%s     — delete word back  %sAlt+D%s    — delete word forward\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+L%s     — clear screen      %sCtrl+Q%s     — exit\n\n", yellow, reset, yellow, reset)
	fmt.Fprintf(out, "%sModes:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sShift+Tab%s  — cycle modes: ai → help → terminal\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+P%s then type \"mode\" — pick a mode in the palette\n\n", yellow, reset)
	fmt.Fprintf(out, "%sExamples:%s\n  show all txt files\n  find errors in logs\n  start docker\n\n", bold, reset)
	fmt.Fprintf(out, "%s Default: %sdry-run=true%s (commands are shown, not executed).\n  Use --dry-run=false to allow low-risk commands, --preview for a review step,\n  or --yes to auto-approve confirmations in scripts.\n\n", bold, green, reset)
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
	fmt.Fprintf(out, "  %sCtrl+N%s     — next command\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+P%s     — command palette (commands and modes)\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+B%s      — back one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+F%s      — forward one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sAlt+D%s      — delete forward one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+W%s     — delete backward one word\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+O%s     — model menu (install / switch on the fly)\n", yellow, reset)
	fmt.Fprintf(out, "  %sCtrl+Q%s     — exit\n", yellow, reset)
	fmt.Fprintf(out, "  %sF1%s         — this help\n", yellow, reset)
	fmt.Fprintf(out, "\n%sModes:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sShift+Tab%s  — cycle modes (ai → help → terminal)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode ai%s   — AI mode (auto-execute)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode help%s — Help mode (command + explanation, nothing is executed)\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode shell%s — Terminal mode (shell inside this window)\n", yellow, reset)
	fmt.Fprintf(out, "\n%sSpecial:%s\n", bold, reset)
	fmt.Fprintf(out, "  %s/exit%s      — exit REPL\n", yellow, reset)
	fmt.Fprintf(out, "  %s/shell%s     — open a terminal session in the app window\n", yellow, reset)
	fmt.Fprintf(out, "  %s/cd%s path   — change directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/clear%s     — clear screen\n", yellow, reset)
	fmt.Fprintf(out, "  %s/pwd%s       — show current directory\n", yellow, reset)
	fmt.Fprintf(out, "  %s/history%s   — show history\n", yellow, reset)
	fmt.Fprintf(out, "  %s/audit%s     — show audit log of executed commands\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show the modes and the current one\n", yellow, reset)
	fmt.Fprintf(out, "  %s/bind%s      — show this list\n", yellow, reset)
	fmt.Fprintf(out, "  %s/setup%s     — show how dmsh connects to the LLM\n", yellow, reset)
	fmt.Fprintf(out, "\n%sCompletion:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sTab%s        — auto-complete slash commands\n", yellow, reset)
}
