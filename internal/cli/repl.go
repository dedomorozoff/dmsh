package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/feedback"
	"github.com/dedomorozoff/dmsh/internal/prompt"
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

var (
	colorReset  = reset
	colorBold   = bold
	colorCyan   = cyan
	colorGreen  = green
	colorYellow = yellow
	colorRed    = red
	colorGray   = gray
)

var errCancelQuestion = errors.New("cancelled")

func handleSlash(line string, out io.Writer, s *session) (stop bool) {
	cfg := &s.cfg
	switch {
	case line == "/exit", line == "/quit", line == "exit", line == "quit":
		fmt.Fprintln(out, "bye!")
		return true
	case line == "/help", line == "help":
		showHelp(out)
	case strings.HasPrefix(line, "/cd "):
		target := strings.TrimSpace(strings.TrimPrefix(line, "/cd "))
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
	case line == "/audit":
		printAuditLimit(out, cfg.AuditFile, 20)
	case line == "/bind", line == "/bind keys":
		showKeyBindings(out)
	case line == "/stats":
		showStats(out, s)
	case line == "/model":
		showModel(out, s)
	case line == "/retry":
		if s.lastInput == "" {
			fmt.Fprintf(out, "%sNo previous request to retry.%s\n", yellow, reset)
			return false
		}
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

func handleTurn(ctx context.Context, s *session, rf *rootFlags, input string, out, errW io.Writer) error {
	input = strings.TrimSpace(input)

	if input == "/retry" {
		if s.lastInput == "" {
			fmt.Fprintf(out, "%sNo previous request to retry.%s\n", yellow, reset)
			return nil
		}
		input = "Alternative approach needed. Previous attempt failed. " + s.lastInput
		fmt.Fprintf(out, "%s[retry]%s %s\n", yellow, reset, s.lastInput)
	}

	if s.cfg.Aliases != nil {
		if expanded, ok := s.cfg.Aliases[input]; ok {
			fmt.Fprintf(out, "%s[alias]%s %s → %s%s%s\n", gray, reset, input, cyan, expanded, reset)
			input = expanded
		}
	}

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
		s.audit(raw, "direct", directDecision(), res)
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

	if s.cfg.Mode == config.ModeShell {
		// Shell mode: run any input directly, never consult the LLM.
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
		s.audit(input, "direct", directDecision(), res)
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

	if resp.Intent != prompt.IntentRunCommand && !(resp.Intent == prompt.IntentExplain && resp.Command != "") && !(resp.Intent == prompt.IntentAskClarification && resp.Command != "") {
		return nil
	}

	if s.cfg.Mode == config.ModeHelp {
		fmt.Fprintf(out, "\n%s%s=== Ready Command ===%s%s\n", bold, green, reset, reset)
		fmt.Fprintf(out, "%s$ %s%s%s\n", cyan, reset, resp.Command, reset)
		if resp.Explanation != "" {
			fmt.Fprintf(out, "\n%s%sExplanation:%s %s\n", bold, yellow, reset, resp.Explanation)
		}
		fmt.Fprintf(out, "%sCopy the command or prefix with ! to execute immediately%s\n", gray, reset)
		return nil
	}

	if rf.cfg.DryRun {
		fmt.Fprintln(out, "(dry-run: command not executed)")
		return nil
	}
	return runCommandWithCorrection(ctx, s, rf, resp, out, errW)
}

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
		if resp.Intent != prompt.IntentAskClarification || strings.TrimSpace(resp.Question) == "" {
			return resp, nil
		}

		fmt.Fprintf(out, "%s[dmsh]%s %s%s%s\n", cyan, reset, cyan, resp.Question, reset)

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

		if strings.HasPrefix(answer, "/") {
			if stop := handleSlash(answer, out, s); stop {
				return resp, context.Canceled
			}
			return resp, ErrSlashCommand
		}

		input = input + "\n" + answer
	}
}

func runCommandWithCorrection(ctx context.Context, s *session, rf *rootFlags, resp prompt.Response, out, errW io.Writer) error {
	// Самозащита: функция никогда не выполняет команды в dry-run-режиме,
	// даже если её вызовут из пути без внешней проверки DryRun.
	if rf.cfg.DryRun {
		fmt.Fprintln(out, "(dry-run: command not executed)")
		return nil
	}

	dec := evaluatePolicy(resp, &rf.cfg)
	if !dec.Allowed {
		fmt.Fprintln(out, "(command blocked by security policy)")
		return nil
	}

	if rf.preview {
		ok, err := previewAndConfirm(out, s, resp.Command, dec.Risk)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "(cancelled)")
			return nil
		}
	} else if dec.Risk != prompt.RiskLow || resp.NeedsConfirmation {
		ok, err := s.confirmOK(out, "execute?")
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
	s.audit(resp.Command, "llm", dec, res)

	fb := feedback.Analyze(resp.Command, res.Stdout, res.Stderr, res.ExitCode)
	if res.Stdout != "" {
		fmt.Fprint(out, res.Stdout)
	}

	if fb.Success {
		if hint := fb.Format(); hint != "" {
			fmt.Fprintf(out, "\n%s[dmsh]%s %s%s%s\n", green, reset, green, hint, reset)
		}
		return nil
	}

	stderr := res.Stderr
	if stderr == "" && res.Err != nil {
		stderr = res.Err.Error()
	}
	// Не засоряем контекст модели гигантским выводом ошибки.
	const maxErrForCorrection = 4096
	if len(stderr) > maxErrForCorrection {
		stderr = stderr[:maxErrForCorrection] + "\n...[truncated]"
	}

	fmt.Fprintf(out, "\n%s[dmsh]%s Error detected (code %d). Requesting auto-correction from LLM...\n", yellow, reset, res.ExitCode)
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

	ok, err := s.confirmOK(out, "execute corrected command?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "(cancelled)")
		return nil
	}

	resCorr := executor.Run(ctx, rf.cfg.Shell, corrResp.Command)
	s.addRecentAndHistory(corrResp.Command, "llm")
	s.audit(corrResp.Command, "llm", decCorr, resCorr)

	fbCorr := feedback.Analyze(corrResp.Command, resCorr.Stdout, resCorr.Stderr, resCorr.ExitCode)
	if resCorr.Stdout != "" {
		fmt.Fprint(out, resCorr.Stdout)
	}

	if hint := fbCorr.Format(); hint != "" {
		if fbCorr.Success {
			fmt.Fprintf(out, "\n%s[dmsh]%s %s%s%s\n", green, reset, green, hint, reset)
		} else {
			fmt.Fprintf(out, "\n%s[dmsh]%s %s%s%s\n", yellow, reset, yellow, hint, reset)
		}
	}

	if resCorr.Err != nil && !fbCorr.Success {
		return fmt.Errorf("exit %d: %w", resCorr.ExitCode, resCorr.Err)
	}
	return nil
}

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

func showModel(out io.Writer, s *session) {
	fmt.Fprintf(out, "\n%s%s=== Model ===%s\n", bold, cyan, reset)
	if s.cfg.ModelPath == "" {
		fmt.Fprintf(out, "  %snone%s\n\n", yellow, reset)
		return
	}
	fmt.Fprintf(out, "  %sIn use:%s  %s\n", bold, reset, s.cfg.ModelPath)
	if fi, err := os.Stat(s.cfg.ModelPath); err == nil {
		fmt.Fprintf(out, "  %sSize:%s    %d MB\n", bold, reset, fi.Size()/1024/1024)
	}
	fmt.Fprintln(out)
}

func showHelp(out io.Writer) {
	fmt.Fprintf(out, "%s%s=== dmsh help ===%s\n\n", bold, cyan, reset)
	fmt.Fprintf(out, "%sDescription:%s\n  dmsh is a natural language shell. Type \"show files\" and it\n  runs \"ls -la\" for you.\n\n", bold, reset)
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
	fmt.Fprintf(out, "  %s/audit%s     — show audit log of executed commands\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show current mode\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode ai%s   or %s/mode 1%s or %s/1%s — AI mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/mode help%s or %s/mode 2%s or %s/2%s — Help mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/mode shell%s or %s/mode 3%s or %s/3%s — Shell mode\n", yellow, reset, yellow, reset, yellow, reset)
	fmt.Fprintf(out, "  %s/stats%s     — session statistics\n", yellow, reset)
	fmt.Fprintf(out, "  %s/model%s     — show the model currently in use\n", yellow, reset)
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
	fmt.Fprintf(out, "%s Default: %sdry-run=false%s (commands execute).\n  Use --dry-run to enable safe mode, --preview for a review step,\n  or --yes to auto-approve confirmations in scripts.\n\n", bold, green, reset)
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
	fmt.Fprintf(out, "  %sCtrl+O%s     — model menu (install / switch on the fly)\n", yellow, reset)
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
	fmt.Fprintf(out, "  %s/audit%s     — show audit log of executed commands\n", yellow, reset)
	fmt.Fprintf(out, "  %s/mode%s      — show current mode\n", yellow, reset)
	fmt.Fprintf(out, "  %s/bind%s      — show this list\n", yellow, reset)
	fmt.Fprintf(out, "  %s!command%s   — execute command directly\n", yellow, reset)
	fmt.Fprintf(out, "\n%sCompletion:%s\n", bold, reset)
	fmt.Fprintf(out, "  %sTab%s        — auto-complete slash commands\n", yellow, reset)
}

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

	if err := copyToClipboard(lastCmd); err != nil {
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
		os.Stderr.Sync()
		if f == os.Stdout || f == os.Stderr {
			os.Stdout.Sync()
		}
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
