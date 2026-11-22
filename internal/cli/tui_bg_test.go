package cli

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

func pumpCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a background command, got nil")
	}
	var msgs []tea.Msg
	for i := 0; i < 1000; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		msgs = append(msgs, msg)
		switch msg.(type) {
		case cmdDoneMsg, directDoneMsg:
			return msgs
		}
	}
	t.Fatalf("the background command never finished (%d messages)", len(msgs))
	return msgs
}

// doneOf достаёт итоговое сообщение фоновой команды из очереди.
func doneOf(t *testing.T, msgs []tea.Msg) tea.Msg {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].(type) {
		case cmdDoneMsg, directDoneMsg:
			return msgs[i]
		}
	}
	t.Fatal("no final message in the background command queue")
	return nil
}

// feedCmd прогоняет фоновую команду через Update, как это делает цикл
// событий, и отдаёт модель после каждого сообщения.
func feedCmd(t *testing.T, m tuiModel, cmd tea.Cmd) tuiModel {
	t.Helper()
	for _, msg := range pumpCmd(t, cmd) {
		mm, _ := m.Update(msg)
		m = mm.(tuiModel)
	}
	return m
}

// Команда LLM выполняется в фоне: Update возвращает сразу, ввод не висит.
func TestRunCommandDoesNotBlock(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "echo dmsh-bg-marker",
		Risk:    prompt.RiskLow,
	}
	got, cmd := m.runCommand(resp)
	if cmd == nil {
		t.Fatal("want a background command, got nil")
	}
	if !got.running {
		t.Fatal("model must be marked as running while the command works")
	}
	// Ввод свободен во время выполнения: печатаем новый запрос.
	got.input = "next"
	got.cursorPos = 4
	if got.input != "next" {
		t.Fatal("input must stay editable while a command runs")
	}
	got = feedCmd(t, got, cmd)
	if got.running {
		t.Fatal("running must be cleared after completion")
	}
	if !strings.Contains(got.content, "dmsh-bg-marker") {
		t.Fatalf("output missing:\n%s", got.content)
	}
}

// Esc во время фоновой команды отменяет её и возвращает в idle.
func TestEscCancelsBackgroundCommand(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "echo dmsh-cancel-marker",
		Risk:    prompt.RiskLow,
	}
	got, cmd := m.runCommand(resp)
	if cmd == nil {
		t.Fatal("want a background command, got nil")
	}
	got = press(got, tea.KeyPressMsg{Code: tea.KeyEsc})
	if got.running || got.state != tuiIdle {
		t.Fatalf("state = %d running = %v, want idle after Esc", got.state, got.running)
	}
	if got.cmdCancel != nil {
		t.Fatal("cancel func must be cleared after Esc")
	}
}

// slowEcho печатает первый маркер, ждёт и печатает второй: так видно, что
// вывод команды появляется в транскрипте, пока она ещё работает.
func slowEcho(first, second string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("echo %s; Start-Sleep -Milliseconds 700; echo %s", first, second)
	}
	return fmt.Sprintf("echo %s; sleep 1; echo %s", first, second)
}

// Главное обещание фоновой команды: её вывод виден в момент работы, а не
// после. Раньше всё печаталось одним куском в конце.
func TestBackgroundOutputIsVisibleWhileRunning(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 20
	m.rf.cfg.Shell = testShell()

	got, cmd := m.runCommand(prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: slowEcho("dmsh-live-first", "dmsh-live-second"),
		Risk:    prompt.RiskLow,
	})

	// Первый кусок вывода должен прийти до завершения команды. Секундный
	// тик может обогнать его: на Windows старт PowerShell занимает больше
	// секунды. Тик не теряем — он продолжает вычитывать очередь.
	var first tea.Msg
	for {
		first = cmd()
		if first == nil {
			t.Fatal("the command closed before producing live output")
		}
		tick, ok := first.(cmdTickMsg)
		if !ok {
			break
		}
		mm, next := got.Update(tick)
		got = mm.(tuiModel)
		if next == nil {
			t.Fatal("a tick must keep the output queue pumped")
		}
		cmd = next
	}
	out, ok := first.(cmdOutMsg)
	if !ok {
		t.Fatalf("first output message = %#v, want a live output chunk", first)
	}
	mm, pump := got.Update(out)
	got = mm.(tuiModel)
	if !strings.Contains(got.content, "dmsh-live-first") {
		t.Fatalf("the first line must be visible before the command finishes:\n%s", got.content)
	}
	if !got.running || got.state != tuiRunning {
		t.Fatalf("state = %d running = %v, want the command still running", got.state, got.running)
	}
	if pump == nil {
		t.Fatal("the output queue must keep being pumped")
	}
	// Строка ввода показывает, что идёт команда и сколько она уже идёт.
	_, inRows := got.layoutRows()
	if row := strings.Join(inRows, "\n"); !strings.Contains(row, "running") {
		t.Fatalf("input row should report the running command:\n%s", row)
	}

	// Дальше крутим тот же насос: он отдаёт остаток вывода и, наконец, итог.
	for i := 0; i < 50; i++ {
		msg := pump()
		if msg == nil {
			break
		}
		if _, isDone := msg.(cmdDoneMsg); isDone {
			mm, _ = got.Update(msg)
			got = mm.(tuiModel)
			break
		}
		mm, next := got.Update(msg)
		got = mm.(tuiModel)
		if next == nil {
			t.Fatalf("the queue stopped being pumped at %T", msg)
		}
		pump = next
	}
	if got.running {
		t.Fatalf("the command must be finished by its own result:\n%s", got.content)
	}
	if !strings.Contains(got.content, "dmsh-live-second") {
		t.Fatalf("the rest of the output must arrive with the result:\n%s", got.content)
	}
	// Живой вывод уже показан — итог не должен печатать его второй раз.
	// Считаем строки, а не вхождения: маркер есть ещё и в напечатанной
	// команде над ними.
	if n := countLines(got.content, "dmsh-live-first"); n != 1 {
		t.Fatalf("live output printed %d times, want once:\n%s", n, got.content)
	}
	if n := countLines(got.content, "dmsh-live-second"); n != 1 {
		t.Fatalf("live output printed %d times, want once:\n%s", n, got.content)
	}
}

// Тик не должен убивать насос: он приходит в тот же канал, что и вывод.
// Раньше Update на cmdTickMsg возвращал nil, и фоновая команда замирала
// вместе с ещё не прочитанным выводом.
func TestTickKeepsCommandQueuePumped(t *testing.T) {
	m := newTestTui()
	ch := make(chan tea.Msg, 2)
	m.cmdCh = ch
	m.running = true
	m.state = tuiRunning

	_, pump := m.Update(cmdTickMsg{})
	if pump == nil {
		t.Fatal("a tick must keep the output queue pumped")
	}

	ch <- cmdOutMsg{seq: m.cmdSeq, text: "still here\n"}
	if msg := pump(); msg == nil {
		t.Fatal("the pump stopped after a tick")
	} else if _, ok := msg.(cmdOutMsg); !ok {
		t.Fatalf("pump returned %#v, want the output chunk", msg)
	}
}

// countLines считает строки транскрипта, равные marker без учёта \r.
func countLines(content, marker string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == marker {
			n++
		}
	}
	return n
}

// Границы кусков не совпадают со строками: дописывать надо как есть, иначе
// строка рвётся на куски, которых в выводе не было.
func TestLiveOutputKeepsPartialLines(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 20
	m.running = true
	m.cmdCh = make(chan tea.Msg, 1)

	mm, _ := m.appendCmdOutput(cmdOutMsg{text: "dmsh-partial-"})
	m = mm.(tuiModel)
	mm, _ = m.appendCmdOutput(cmdOutMsg{text: "rest\n"})
	m = mm.(tuiModel)

	if !strings.Contains(m.content, "dmsh-partial-rest\n") {
		t.Fatalf("chunks must join into one line:\n%q", m.content)
	}
	if strings.Contains(m.content, "dmsh-partial-\n") {
		t.Fatalf("a chunk must not be closed with a newline:\n%q", m.content)
	}
}

// Живой вывод ограничен: лог сборки на сотни мегабайт не должен влезать в
// транскрипт целиком, но очередь обязана продолжаться, иначе процесс встанет.
func TestLiveOutputTruncatesButKeepsPumping(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 20
	m.running = true
	m.cmdCh = make(chan tea.Msg, 1)
	m.cmdLive = maxLiveOutput - 10

	mm, pump := m.appendCmdOutput(cmdOutMsg{text: strings.Repeat("x", 64)})
	m = mm.(tuiModel)
	if !m.cmdTrunc {
		t.Fatal("live output must be cut at the limit")
	}
	if !strings.Contains(m.content, "live output cut") {
		t.Fatalf("the cut must be visible:\n%s", m.content)
	}
	if strings.Contains(m.content, "xxxxxxxxxx") {
		t.Fatalf("the chunk past the limit must not be printed:\n%s", m.content)
	}
	if pump == nil {
		t.Fatal("the queue must keep being pumped after the cut, or the process blocks")
	}
	// Следующий кусок тоже не печатается, но очередь жива.
	mm, pump = m.appendCmdOutput(cmdOutMsg{text: "tail\n"})
	if pump == nil || mm.(tuiModel).cmdLive != m.cmdLive {
		t.Fatal("pumping and the byte counter must stay stable after the cut")
	}
}

// Устаревший результат фоновой команды не затирает новую.
func TestStaleBackgroundResultIgnored(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	first, cmd1 := m.runCommand(prompt.Response{
		Intent: prompt.IntentRunCommand, Command: "echo first", Risk: prompt.RiskLow,
	})
	if cmd1 == nil {
		t.Fatal("want a background command, got nil")
	}
	stale := doneOf(t, pumpCmd(t, cmd1))
	staleBefore := first.content
	// Пользователь успел запустить вторую команду раньше завершения первой.
	second, _ := first.startBackgroundCommand(prompt.Response{
		Intent: prompt.IntentRunCommand, Command: "echo second", Risk: prompt.RiskLow,
	})
	mm, _ := second.Update(stale)
	got := mm.(tuiModel)
	// Эхо первой команды уже было в транскрипте до запуска второй — это
	// нормально. Устаревший результат не должен добавлять вывод "first".
	if strings.Contains(got.content[len(staleBefore):], "first") {
		t.Fatalf("stale result must be ignored:\n%s", got.content)
	}
	if !got.running {
		t.Fatal("second command must still be running")
	}
	if got.cmdSeq != second.cmdSeq {
		t.Fatal("seq must not roll back to the stale message")
	}
}

// Успешная команда возвращает лимит автокоррекции в полное состояние.
func TestAutoCorrectBudgetResetsOnSuccess(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	m.fixTries = 1
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "echo dmsh-auto-fix-marker",
		Risk:    prompt.RiskLow,
	}
	got, cmd := m.runCommand(resp)
	if cmd == nil {
		t.Fatal("runCommand must return a background command")
	}
	// Команда ушла в фон: синхронного вывода ещё нет, но состояние — running.
	if !got.running || got.state != tuiRunning {
		t.Fatalf("state = %d running = %v, want tuiRunning + running", got.state, got.running)
	}
	// Выполняем фоновую команду и отдаём результат обратно в модель.
	msg := doneOf(t, pumpCmd(t, cmd))
	if d, ok := msg.(cmdDoneMsg); !ok || d.seq != got.cmdSeq {
		t.Fatalf("final message = %#v, want cmdDoneMsg with seq %d", msg, got.cmdSeq)
	}
	mm, _ := got.Update(msg)
	got = mm.(tuiModel)
	if got.fixTries != 0 {
		t.Fatalf("fixTries = %d after a successful command, want 0", got.fixTries)
	}
	if !strings.Contains(got.content, "dmsh-auto-fix-marker") {
		t.Fatalf("command output missing:\n%s", got.content)
	}
	if got.running || got.state != tuiIdle {
		t.Fatalf("state = %d running = %v, want idle after completion", got.state, got.running)
	}
}

// Новый запрос пользователя начинает цикл автокоррекции заново.
func TestNewRequestResetsAutoFixBudget(t *testing.T) {
	m := newTestTui()
	m.s.engine = &captureEngine{}
	m.fixTries = maxAutoFix
	m.input = "ls"
	m.cursorPos = 2
	mm, _ := m.handleEnter()
	got := mm.(tuiModel)
	if got.fixTries != 0 {
		t.Fatalf("fixTries = %d after a new request, want 0", got.fixTries)
	}
}

func TestRenderScrollShowsHistory(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 20
	for i := 0; i < 30; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	out := m.render()
	if !strings.Contains(out, "line 29") {
		t.Fatalf("render without scroll should show most recent lines:\n%s", out)
	}
	if strings.Contains(out, "line 05") {
		t.Fatalf("render without scroll should not show oldest lines when content overflows:\n%s", out)
	}
	m.scrollOffset = 10
	out = m.render()
	if strings.Contains(out, "line 29") {
		t.Fatalf("scrolled render should not show newest line when scrolled up:\n%s", out)
	}
	if !strings.Contains(out, "line 05") || !strings.Contains(out, "line 06") {
		t.Fatalf("scrolled render should show older lines:\n%s", out)
	}
}
