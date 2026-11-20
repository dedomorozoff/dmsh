package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/feedback"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// waitForCmd ждёт следующего сообщения фоновой команды: куска вывода или
// итога. Канал закрывается вместе с командой, и тогда команда возвращает
// nil — лишнего сообщения в Update не будет.
func waitForCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// startBackgroundCmd готовит канал и горутину фоновой команды. Один запуск —
// один канал: следующая команда получает свой, и устаревшие куски вывода
// отбрасываются по seq. Тик секунды едет тем же каналом — по нему в строке
// ввода видно, что команда жива и сколько уже идёт.
func (m *tuiModel) startBackgroundCmd(shell, command string, onDone func(executor.Result) tea.Msg) {
	ch := make(chan tea.Msg, 64)
	m.cmdCh = ch
	m.cmdLive, m.cmdTrunc = 0, false

	ctx, cancel := context.WithCancel(context.Background())
	m.cmdCancel = cancel
	seq := m.cmdSeq

	go func() {
		ticks := time.NewTicker(time.Second)
		var ticker sync.WaitGroup
		ticker.Add(1)
		go func() {
			defer ticker.Done()
			tickCmdLoop(ch, ticks.C, ctx.Done())
		}()

		res := executor.RunStream(ctx, shell, command, func(text string, isErr bool) {
			if text == "" {
				return
			}
			select {
			case ch <- cmdOutMsg{seq: seq, text: text, isErr: isErr}:
			case <-ctx.Done():
			}
		})
		select {
		case ch <- onDone(res):
		case <-ctx.Done():
		}
		// Канал закрываем только после остановки тикера: отправка в
		// закрытый канал паникует, а тикер как раз шлёт в него.
		ticks.Stop()
		cancel()
		ticker.Wait()
		close(ch)
	}()
}

// tickCmdLoop шлёт в канал команды секундный тик, пока команда жива. Тик едет
// тем же каналом, что и вывод: иначе Update пришлось бы возвращать сразу
// две команды, и очередь вывода перестала бы продвигаться между тиками.
func tickCmdLoop(ch chan<- tea.Msg, ticks <-chan time.Time, done <-chan struct{}) {
	for {
		select {
		case <-ticks:
			select {
			case ch <- cmdTickMsg{}:
			case <-done:
				return
			}
		case <-done:
			return
		}
	}
}

func (m tuiModel) submitToLLM(input string) (tea.Model, tea.Cmd) {
	return m.submitToLLMEcho(input, input)
}

// submitToLLMEcho отправляет модели send, но в транскрипт печатает echo.
// Для уточнений это разные строки: модели уходит вопрос с ответом, а
// пользователю — только его ответ.
func (m tuiModel) submitToLLMEcho(send, echo string) (tea.Model, tea.Cmd) {
	// Новый запрос — новый контекст: панель «что делать дальше» относится к
	// предыдущей команде и в середине нового разговора только сбивает.
	m.actionCmd = ""
	// Echo the request into the transcript so it does not vanish when the
	// input line is cleared on enter.
	m.addLine(fmt.Sprintf("%s> %s%s", colorGray, echo, colorReset))
	mm, cmd := m.startLLMStream("run", send)
	return mm, cmd
}

// startLLMStream запускает фоновый стриминг LLM и возвращает команду,
// которая доставляет токены и результат в Update. Каналы НЕ закрываются,
// чтобы avoid busy-loop на закрытом канале; завершение сигналится через done.
func (m tuiModel) startLLMStream(mode, input string) (tuiModel, tea.Cmd) {
	m.streamCtx, m.streamCancel = context.WithCancel(context.Background())
	tokenCh := make(chan tokenMsg, 256)
	doneCh := make(chan streamResult, 1)
	m.tokenCh = tokenCh
	m.streamDone = doneCh
	m.streaming = true
	m.streamed = false
	m.streamHasContent = false
	m.state = tuiStreaming

	var buf strings.Builder
	w := &tuiWriter{buf: &buf, tokenCh: tokenCh}

	go func() {
		resp, raw, err := m.s.askStream(m.streamCtx, mode, input, w)
		if buf.Len() > 0 {
			select {
			case tokenCh <- tokenMsg{text: buf.String()}:
			default:
			}
			buf.Reset()
		}
		doneCh <- streamResult{resp: resp, raw: raw, err: err}
	}()

	return m, waitForToken(tokenCh, doneCh)
}

func (m tuiModel) executeResponse(resp prompt.Response) (tuiModel, tea.Cmd) {
	if m.rf.cfg.DryRun {
		m.addLine("(dry-run: command not executed)")
		return m, nil
	}
	if m.rf.cfg.Mode == config.ModeHelp {
		m.addLine(fmt.Sprintf("\n%s=== Ready Command ===%s", colorBold+colorGreen, colorReset))
		m.addLine(fmt.Sprintf("$ %s", resp.Command))
		if resp.Explanation != "" {
			m.addLine(fmt.Sprintf("\n%sExplanation:%s %s", colorBold+colorYellow, colorReset, resp.Explanation))
		}
		m.addLine(fmt.Sprintf("%sCopy the command or prefix with ! to execute immediately%s", colorGray, colorReset))
		return m, nil
	}
	dec := evaluatePolicy(resp, &m.rf.cfg)
	if !dec.Allowed {
		reason := strings.TrimSpace(dec.Reason)
		if reason == "" {
			reason = "security policy"
		}
		m.addLine(fmt.Sprintf("(blocked by security policy: %s)", reason))
		return m, nil
	}
	if dec.Risk != prompt.RiskLow || resp.NeedsConfirmation {
		lite := policyDecisionLite{allowed: dec.Allowed, risk: dec.Risk, reason: dec.Reason}
		return m.openConfirm(resp, lite), nil
	}
	return m.runCommand(resp)
}

func (m tuiModel) runCommand(resp prompt.Response) (tuiModel, tea.Cmd) {
	m.addLine(fmt.Sprintf("\n%s$ %s%s", colorCyan, resp.Command, colorReset))
	m.addLine(fmt.Sprintf("%s%s%s", gray, strings.Repeat("─", 40), colorReset))
	if handled, shouldExit, err := runBuiltin(resp.Command, io.Discard, io.Discard, m.s.recent); handled {
		if err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
		if shouldExit {
			m.addLine("bye!")
		}
		m.s.addRecentAndHistory(resp.Command, "llm")
		return m, nil
	}
	// Долгие команды (composer, тесты, сборка) идут в фоне: TUI остаётся
	// отзывчивым, вывод появится по завершении. Отмена — Esc/Ctrl+C.
	return m.startBackgroundCommand(resp)
}

// startBackgroundCommand запускает команду LLM в фоне через горутину.
// Синхронного executor.Run здесь больше нет: раньше Update висел на нём,
// и весь интерфейс замирал до конца команды. Вывод приходит кусками, пока
// команда пишет, — отмена — Esc/Ctrl+C.
func (m tuiModel) startBackgroundCommand(resp prompt.Response) (tuiModel, tea.Cmd) {
	m.cancelCommand()
	m.cmdSeq++
	m.cmdCommand = resp.Command
	m.cmdStartedAt = time.Now()
	m.running = true
	m.state = tuiRunning
	dec := evaluatePolicy(resp, &m.rf.cfg)
	lite := policyDecisionLite{allowed: dec.Allowed, risk: dec.Risk, reason: dec.Reason}
	shell := m.rf.cfg.Shell
	command := resp.Command
	m.addLine(fmt.Sprintf("%srunning in background — output appears as it comes, Esc cancels%s", colorGray, colorReset))
	seq := m.cmdSeq
	m.startBackgroundCmd(shell, command, func(res executor.Result) tea.Msg {
		return cmdDoneMsg{seq: seq, resp: resp, res: res, dec: lite}
	})
	return m, m.cmdPump()
}

// cmdPump держит очередь фоновой команды: каждое принятое сообщение требует
// следующего ожидания, иначе вывод перестанет приходить на середине.
func (m tuiModel) cmdPump() tea.Cmd {
	if m.cmdCh == nil {
		return nil
	}
	return waitForCmd(m.cmdCh)
}

// appendCmdOutput печатает кусок живого вывода. Позиция скролла не
// трогается: если пользователь ушёл читать выше, новые строки не должны
// выбрасывать его вниз.
func (m tuiModel) appendCmdOutput(msg cmdOutMsg) (tea.Model, tea.Cmd) {
	if m.cmdCh == nil {
		return m, nil
	}
	if msg.seq == m.cmdSeq && m.running {
		switch {
		case m.cmdTrunc:
			// Вывод уже обрезан: дальше только качаем очередь, иначе
			// процесс встанет на переполненном канале.
		case m.cmdLive+len(msg.text) > maxLiveOutput:
			m.cmdTrunc = true
			m.addLine(fmt.Sprintf("%s(live output cut after %d KB — the full text stays in the audit log)%s",
				colorGray, maxLiveOutput/1024, colorReset))
		default:
			m.cmdLive += len(msg.text)
			if msg.isErr {
				m.appendLive(colorRed + msg.text + colorReset)
			} else {
				m.appendLive(msg.text)
			}
		}
	}
	return m, m.cmdPump()
}

// maxLiveOutput ограничивает живой вывод в транскрипте: лог сборки может
// занимать мегабайты, а кадр перерисовывается целиком на каждом тике.
const maxLiveOutput = 64 * 1024

// cmdElapsed — сколько команда идёт, для строки ввода.
func (m tuiModel) cmdElapsed() string {
	if m.cmdStartedAt.IsZero() {
		return "0s"
	}
	return time.Since(m.cmdStartedAt).Round(time.Second).String()
}

// cancelCommand останавливает фоновую команду, если она идёт. Канал
// отпускается вместе с ней: куски вывода уже некуда принимать, а процесс
// увидит отмену и перестанет писать.
func (m *tuiModel) cancelCommand() {
	if m.cmdCancel != nil {
		m.cmdCancel()
		m.cmdCancel = nil
	}
	m.cmdCh = nil
	m.running = false
}

// finishBackgroundCommand печатает результат фоновой команды и ведёт
// историю/аудит тем же путём, что и синхронный запуск.
func (m tuiModel) finishBackgroundCommand(msg cmdDoneMsg) (tuiModel, tea.Cmd) {
	if msg.seq != m.cmdSeq {
		return m, nil
	}
	// Показанное живьём печатать второй раз незачем, а для длинного лога это
	// просто удвоенный транскрипт. Решение принимаем до cancelCommand: тот
	// обнуляет счётчики живого вывода.
	streamed := m.cmdLive > 0 || m.cmdTrunc
	m.cancelCommand()
	m.state = tuiIdle
	elapsed := time.Since(m.cmdStartedAt).Round(time.Second)
	m.s.addRecentAndHistory(msg.resp.Command, "llm")
	m.s.audit(msg.resp.Command, "llm", restoreDecision(msg.dec), msg.res)
	m.actionCmd = msg.resp.Command
	if !streamed {
		if msg.res.Stdout != "" {
			m.addLine(msg.res.Stdout)
		}
		if msg.res.Stderr != "" {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.res.Stderr, colorReset))
		}
	}
	if msg.res.Err != nil && msg.res.ExitCode != 0 {
		m.addLine(fmt.Sprintf("%sexit %d after %s%s", colorGray, msg.res.ExitCode, elapsed, colorReset))
	} else {
		m.addLine(fmt.Sprintf("%sdone in %s%s", colorGray, elapsed, colorReset))
	}
	fb := feedback.Analyze(msg.resp.Command, msg.res.Stdout, msg.res.Stderr, msg.res.ExitCode)
	if fb.Success {
		// Команда удалась — цикл автокоррекции закончен, лимит снова полон.
		m.fixTries = 0
		if hint := fb.Format(); hint != "" {
			m.addLine(fmt.Sprintf("\n%s[dmsh]%s %s%s%s", colorGreen, colorReset, colorGreen, hint, colorReset))
		}
		return m, nil
	}
	return m.autoCorrect(msg.resp, msg.res)
}

// restoreDecision возвращает полное решение политики из лёгкой копии:
// нужно для аудита фоновой команды после её завершения.
func restoreDecision(lite policyDecisionLite) policy.Decision {
	return policy.Decision{Allowed: lite.allowed, Risk: lite.risk, Reason: lite.reason}
}

// maxAutoFix ограничивает автокоррекцию подряд. Модель может сколько угодно
// возвращать неработающую команду; без предела каждая попытка — это новый
// запрос к модели и новый запуск команды, то есть бесконечный цикл.
const maxAutoFix = 2

// autoCorrect просит модель исправить упавшую команду. Число попыток
// ограничено: исчерпав лимит, останавливаемся и показываем, что делать
// дальше, вместо того чтобы крутиться.
func (m tuiModel) autoCorrect(resp prompt.Response, res executor.Result) (tuiModel, tea.Cmd) {
	stderr := res.Stderr
	if stderr == "" && res.Err != nil {
		stderr = res.Err.Error()
	}
	m.fixTries++
	if m.fixTries > maxAutoFix {
		m.fixTries = 0
		m.state = tuiIdle
		m.addLine(fmt.Sprintf("%s[dmsh]%s the command failed %d times, giving up on auto-fix", colorRed, colorReset, maxAutoFix))
		if stderr != "" {
			m.addLine(fmt.Sprintf("%s%s%s", colorGray, stderr, colorReset))
		}
		m.addLine(fmt.Sprintf("%srun the command yourself or rephrase the request%s", colorGray, colorReset))
		return m, nil
	}

	m.addLine(fmt.Sprintf("\n%s[dmsh]%s Error detected (code %d). Requesting auto-correction from LLM (attempt %d/%d)...%s",
		colorYellow, colorReset, res.ExitCode, m.fixTries, maxAutoFix, colorReset))
	m.addLine(fmt.Sprintf("%s%s%s", gray, strings.Repeat("─", 40), colorReset))
	m.s.addFixNote()
	correctionInput := fmt.Sprintf("Command '%s' failed.\nExit code: %d\nStderr:\n%s\n\nPlease fix the command so it runs successfully on the current OS.", resp.Command, res.ExitCode, stderr)
	// Автокоррекция — тоже новый запрос, уточнения сбрасываются.
	m.clarify = clarification{}
	return m.startLLMStream("run", correctionInput)
}

// startDirectCommand запускает "!"-команду в фоне. Встроенные (cd, exit)
// выполняются сразу — им фон не нужен.
func (m tuiModel) startDirectCommand(cmd string) (tuiModel, tea.Cmd) {
	if handled, shouldExit, err := runBuiltin(cmd, io.Discard, io.Discard, m.s.recent); handled {
		if err != nil {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, err, colorReset))
		}
		if shouldExit {
			m.addLine("bye!")
		}
		m.s.addRecentAndHistory(cmd, "direct")
		return m, nil
	}
	m.cancelCommand()
	m.cmdSeq++
	m.cmdCommand = cmd
	m.cmdStartedAt = time.Now()
	m.running = true
	m.state = tuiRunning
	shell := m.rf.cfg.Shell
	m.addLine(fmt.Sprintf("%srunning in background — output appears as it comes, Esc cancels%s", colorGray, colorReset))
	seq := m.cmdSeq
	m.startBackgroundCmd(shell, cmd, func(res executor.Result) tea.Msg {
		return directDoneMsg{seq: seq, cmd: cmd, res: res}
	})
	return m, m.cmdPump()
}

// finishDirectCommand печатает результат фоновой "!"-команды.
func (m tuiModel) finishDirectCommand(msg directDoneMsg) (tuiModel, tea.Cmd) {
	if msg.seq != m.cmdSeq {
		return m, nil
	}
	// Как и у команды LLM: живой вывод уже в транскрипте, второй раз не
	// печатаем. Счётчики смотрим до cancelCommand.
	streamed := m.cmdLive > 0 || m.cmdTrunc
	m.cancelCommand()
	m.state = tuiIdle
	elapsed := time.Since(m.cmdStartedAt).Round(time.Second)
	m.s.addRecentAndHistory(msg.cmd, "direct")
	m.s.audit(msg.cmd, "direct", directDecision(), msg.res)
	m.actionCmd = msg.cmd
	if !streamed {
		if msg.res.Stdout != "" {
			m.addLine(msg.res.Stdout)
		}
		if msg.res.Stderr != "" {
			m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.res.Stderr, colorReset))
		}
	}
	if msg.res.Err != nil {
		m.addLine(fmt.Sprintf("%sexit %d: %v%s", colorRed, msg.res.ExitCode, msg.res.Err, colorReset))
	} else {
		m.addLine(fmt.Sprintf("%sdone in %s%s", colorGray, elapsed, colorReset))
	}
	return m, nil
}

func waitForToken(ch <-chan tokenMsg, done <-chan streamResult) tea.Cmd {
	return func() tea.Msg {
		select {
		case tok := <-ch:
			return tok
		case res := <-done:
			return streamDoneMsg{resp: res.resp, err: res.err}
		}
	}
}

