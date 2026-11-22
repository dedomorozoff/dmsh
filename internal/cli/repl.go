package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/feedback"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// Цветовые escape-коды ANSI. Используются во всём пакете cli.
var (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorGray   = "\033[90m"
)

// Псевдонимы для обратной совместимости с кодом в session.go и oneshot.go.
var (
	reset  = colorReset
	bold   = colorBold
	cyan   = colorCyan
	green  = colorGreen
	yellow = colorYellow
	red    = colorRed
	gray   = colorGray
)

// errCancelQuestion сигнализирует об отмене clarification-вопроса.
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
	case line == "/todo", line == "todo":
		showTodo(out, s)
	case strings.HasPrefix(line, "/todo "):
		handleTodo(strings.TrimSpace(strings.TrimPrefix(line, "/todo ")), out, s)
	case line == "/stats":
		showStats(out, s)
	case line == "/model":
		showModel(out, s)
	case line == "/models":
		showModels(out, s)
	case line == "/settings", line == "/proxy":
		showSettings(out, s)
	case line == "/setup":
		showSetup(out, s)
	case line == "/mouse", strings.HasPrefix(line, "/mouse "):
		want := !s.cfg.Mouse
		if arg := strings.TrimSpace(strings.TrimPrefix(line, "/mouse")); arg != "" {
			switch strings.ToLower(arg) {
			case "on", "true", "1", "yes":
				want = true
			case "off", "false", "0", "no":
				want = false
			default:
				fmt.Fprintf(out, "%susage: /mouse [on|off]%s\n", yellow, reset)
				return false
			}
		}
		s.cfg.Mouse = want
		if err := config.Save(s.cfg); err != nil {
			fmt.Fprintf(out, "%sMouse setting applied for this session, but could not save config: %v%s\n", yellow, err, reset)
		}
		if want {
			fmt.Fprintln(out, "Mouse: on. In TUI, run /mouse off to enable native text selection/copy.")
		} else {
			fmt.Fprintln(out, "Mouse: off. In TUI, native terminal text selection/copy is enabled; run /mouse on to restore UI clicks.")
		}
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

func askWithFollowUp(ctx context.Context, s *session, mode, input string, out, errW io.Writer) (prompt.Response, error) {
	var clarify clarification
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

		// Модель переспрашивает, а уточнений уже было достаточно: не
		// мучаем пользователя, показываем, чего от него ждали.
		if clarify.exhausted() || clarify.repeatsLast(resp.Question) {
			fmt.Fprintf(out, "%s(stopped: the model kept asking instead of answering — rephrase the request)%s\n", gray, reset)
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

		answer = strings.TrimSpace(stripListMarker(answer))
		input = clarify.followUp(input, resp.Question, answer)
	}
}

// stripListMarker убирает нумерацию, которую терминалы и редакторы любят
// добавлять к ответам на вопросы: "1. project-name" должно уйти в модели
// как есть. После маркера обязателен пробел, иначе пострадали бы версии
// вида "3.5" и "1.2.3".
func stripListMarker(answer string) string {
	answer = strings.TrimSpace(answer)
	i := 0
	for i < len(answer) && answer[i] >= '0' && answer[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(answer) {
		return answer
	}
	if answer[i] != '.' && answer[i] != ')' {
		return answer
	}
	if answer[i+1] != ' ' && answer[i+1] != '\t' {
		return answer
	}
	rest := strings.TrimSpace(answer[i+1:])
	if rest == "" {
		return answer
	}
	return rest
}

func runCommandWithCorrection(ctx context.Context, s *session, rf *rootFlags, resp prompt.Response, out, errW io.Writer) error {
	// Самозащита: функция никогда не выполняет команды в dry-run-режиме,
	// даже если её вызовут из пути без внешней проверки DryRun.
	if rf.cfg.DryRun {
		fmt.Fprintln(out, dryRunNotice)
		return nil
	}

	dec := evaluatePolicy(resp, &rf.cfg)
	if !dec.Allowed {
		reason := strings.TrimSpace(dec.Reason)
		if reason == "" {
			reason = "security policy"
		}
		fmt.Fprintf(out, "(blocked by security policy: %s)\n", reason)
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
		reason := strings.TrimSpace(decCorr.Reason)
		if reason == "" {
			reason = "security policy"
		}
		fmt.Fprintf(out, "(corrected command blocked by security policy: %s)\n", reason)
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
		return &ExitError{Code: resCorr.ExitCode, Err: resCorr.Err}
	}
	return nil
}
