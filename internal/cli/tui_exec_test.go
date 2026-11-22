package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

type captureEngine struct {
	tokens       []string
	gen          string
	system, user string
}

func (e *captureEngine) Generate(_ context.Context, system, user string, _ llm.SamplingOptions) (string, error) {
	e.system, e.user = system, user
	return e.gen, nil
}

func (e *captureEngine) Stream(_ context.Context, system, user string, _ llm.SamplingOptions, tokens chan<- string) error {
	e.system, e.user = system, user
	for _, t := range e.tokens {
		tokens <- t
	}
	close(tokens)
	return nil
}

func (*captureEngine) Close() error { return nil }

func TestCtrlOOpensModelMenu(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.state != tuiModelMenu {
		t.Fatalf("state = %d, want tuiModelMenu", m.state)
	}
	if len(m.modelItems) == 0 {
		t.Fatal("model menu should list items")
	}
	_, rows := m.layoutRows()
	if len(rows) == 0 {
		t.Fatal("model menu should render rows")
	}
}

func TestModelMenuNavigateUpDown(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	start := m.modelIdx
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.modelIdx != (start+1)%len(m.modelItems) {
		t.Fatalf("modelIdx = %d after down, want %d", m.modelIdx, (start+1)%len(m.modelItems))
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.modelIdx != start {
		t.Fatalf("modelIdx = %d after up, want %d", m.modelIdx, start)
	}
}

func TestModelMenuEscapeCloses(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m.input = "hello"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %d after esc, want tuiIdle", m.state)
	}
	if m.input != "hello" {
		t.Fatalf("input = %q, want it preserved", m.input)
	}
}

func TestModelMenuEnterLoadsInstalled(t *testing.T) {
	m := newTestTui()
	m.state = tuiModelMenu
	m.modelItems = []modelMenuItem{{name: "test-model.gguf", installed: true}}
	m.modelIdx = 0
	mm, cmd := m.menuEnter()
	if cmd == nil {
		t.Fatal("menuEnter should return a pump command while loading")
	}
	loaded := mm.(tuiModel)
	if !loaded.modelBusy {
		t.Fatal("modelBusy should be true while loading")
	}
	if loaded.modelStatus != "loading test-model.gguf" {
		t.Fatalf("modelStatus = %q", loaded.modelStatus)
	}
}

// Печатные клавиши фильтруют список моделей, а не закрывают окно: каталог
// удалённого провайдера длинный, и искать его иначе нечем.
func TestModelMenuTypeKeyFiltersList(t *testing.T) {
	m := newTestTui()
	m = press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.state != tuiModelMenu {
		t.Fatalf("state = %d, want tuiModelMenu", m.state)
	}
	m.modelItems = []modelMenuItem{
		{name: "qwen3:8b", installed: true},
		{name: "qwen2.5-coder:7b", installed: true},
		{name: "deepcoder:1.5b", installed: true},
	}
	m = press(m, keyText("qwen2"))
	if m.state != tuiModelMenu {
		t.Fatalf("typing must keep the menu open, state = %d", m.state)
	}
	if got := m.visibleModelItems(); len(got) != 1 || got[0].name != "qwen2.5-coder:7b" {
		t.Fatalf("filtered = %+v, want only qwen2.5-coder:7b", got)
	}
	if !strings.Contains(ansi.Strip(m.render()), "filter: qwen2") {
		t.Fatalf("the filter should be visible:\n%s", m.render())
	}
	// Esc снимает фильтр и только потом закрывает окно.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiModelMenu || m.modelFilter != "" {
		t.Fatalf("esc must clear the filter first, state = %d filter = %q", m.state, m.modelFilter)
	}
	if got := m.visibleModelItems(); len(got) != 3 {
		t.Fatalf("after clearing the filter: %d items, want 3", len(got))
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("a second esc must close the menu, state = %d", m.state)
	}
}

func TestSessionSwitchModelSwapsEngine(t *testing.T) {
	s := &session{
		cfg:    config.Config{ModelPath: "/old.gguf", Threads: 2, CtxSize: 2048, GPULayers: 0},
		engine: &captureEngine{},
	}
	if err := s.switchModel("/new.gguf"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	if s.cfg.ModelPath != "/new.gguf" {
		t.Fatalf("cfg.ModelPath = %q, want /new.gguf", s.cfg.ModelPath)
	}
	if _, ok := s.engine.(*captureEngine); ok {
		t.Fatal("old engine should have been replaced")
	}
}

func TestScrollUpDownWhenInputEmpty(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 50; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	if m.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", m.scrollOffset)
	}
	m.scrollUp()
	if m.scrollOffset != 1 {
		t.Fatalf("scrollUp once: scrollOffset = %d, want 1", m.scrollOffset)
	}
	m.scrollDown()
	if m.scrollOffset != 0 {
		t.Fatalf("scrollDown after scrollUp: scrollOffset = %d, want 0", m.scrollOffset)
	}
	m.scrollDown()
	if m.scrollOffset != 0 {
		t.Fatalf("scrollDown at bottom: scrollOffset = %d, want 0", m.scrollOffset)
	}
	for i := 0; i < 20; i++ {
		m.scrollUp()
	}
	if m.scrollOffset != 20 {
		t.Fatalf("scrollUp 20 times: scrollOffset = %d, want 20", m.scrollOffset)
	}
}

func TestScrollPgUpPgDown(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 100; i++ {
		m.addLine(fmt.Sprintf("line %03d", i))
	}
	// Scroll to near the top first.
	m.scrollOffset = 90
	// PgUp should move up.
	m.scrollBy(-(m.height - 3))
	if m.scrollOffset >= 90 {
		t.Fatalf("scrollBy PgUp: scrollOffset = %d, want < 90", m.scrollOffset)
	}
	// PgDown should move back down.
	m.scrollBy(m.height - 3)
	if m.scrollOffset != 90 {
		t.Fatalf("scrollBy PgDown back: scrollOffset = %d, want 90", m.scrollOffset)
	}
}

func TestArrowsDoNotScrollWhileTyping(t *testing.T) {
	m := newTestTui()
	m.input = "hello"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "hello" {
		t.Fatalf("empty history must leave the input alone, got %q", m.input)
	}
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset changed while typing: %d", m.scrollOffset)
	}
}

// Стрелки листают историю всегда, в том числе на пустой строке ввода:
// иначе ↑ на пустом промпте (самый частый случай) не работает вовсе.
func TestArrowsWalkHistoryOnEmptyInput(t *testing.T) {
	m := newTestTui()
	m.history = []string{"first", "second"}
	m.histIdx = len(m.history)

	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "second" {
		t.Fatalf("up on empty input: input = %q, want %q", m.input, "second")
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "first" {
		t.Fatalf("second up: input = %q, want %q", m.input, "first")
	}
	// Выше начала не заворачиваем.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "first" {
		t.Fatalf("up past the start wrapped to %q", m.input)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "second" {
		t.Fatalf("down: input = %q, want %q", m.input, "second")
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input != "" {
		t.Fatalf("down past the end = %q, want empty", m.input)
	}
}

// Стрелки не должны двигать скролл: скролл живёт на PgUp/PgDn.
func TestArrowsDoNotScroll(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 50; i++ {
		m.addLine(fmt.Sprintf("line %02d", i))
	}
	m.history = []string{"cmd"}
	m.histIdx = 1
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.scrollOffset != 0 {
		t.Fatalf("up must not scroll, scrollOffset = %d", m.scrollOffset)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.scrollOffset != 0 {
		t.Fatalf("down must not scroll, scrollOffset = %d", m.scrollOffset)
	}
}

// PgUp/PgDn скроллят вывод независимо от того, есть ли текст в строке ввода.
func TestPgUpPgDownScrollAlways(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.height = 10
	for i := 0; i < 100; i++ {
		m.addLine(fmt.Sprintf("line %03d", i))
	}
	m.scrollOffset = 90
	m.input = "half-typed"
	m = press(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.scrollOffset >= 90 {
		t.Fatalf("pgup should scroll even with text in the input: %d", m.scrollOffset)
	}
	before := m.scrollOffset
	m = press(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.scrollOffset <= before {
		t.Fatalf("pgdown should scroll forward: %d -> %d", before, m.scrollOffset)
	}
}

// История подтягивается из файла прошлых сессий: без этого стрелки видят
// только текущую сессию.
func TestHistoryLoadsFromFile(t *testing.T) {
	dir := t.TempDir()
	histFile := filepath.Join(dir, "history.jsonl")
	write := func(cmds ...string) {
		var sb strings.Builder
		for _, c := range cmds {
			data, _ := json.Marshal(HistoryEntry{Timestamp: time.Now(), Command: c, Source: "direct"})
			sb.WriteString(string(data) + "\n")
		}
		if err := os.WriteFile(histFile, []byte(sb.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Подряд идущие дубликаты схлопываются. Неподрядные повторы сохраняются:
	// один и тот же Enter дважды подряд — один шаг истории, а та же команда
	// вперемешку с другими — это два разных запуска.
	write("old-cmd", "old-cmd", "older-cmd", "old-cmd")

	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI}}
	s := &session{cfg: config.Config{Mode: config.ModeAI, HistoryFile: histFile}}
	m := NewTuiModel(rf, s).(tuiModel)

	// Порядок файла сохранён как есть; схлопнулась только соседняя пара.
	want := []string{"old-cmd", "older-cmd", "old-cmd"}
	if len(m.history) != len(want) {
		t.Fatalf("history = %v, want %v", m.history, want)
	}
	for i, w := range want {
		if m.history[i] != w {
			t.Fatalf("history = %v, want %v", m.history, want)
		}
	}
	if m.histIdx != len(m.history) {
		t.Fatalf("histIdx = %d, want %d (past the end, ready to walk back)", m.histIdx, len(m.history))
	}
	// Стрелка вверх сразу подставляет последнюю выполненную команду.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input != "old-cmd" {
		t.Fatalf("up after load: input = %q, want %q", m.input, "old-cmd")
	}
}

// Повтор той же команды не раздувает историю.
func TestRepeatedCommandNotDuplicated(t *testing.T) {
	m := newTestTui()
	m.history = []string{"ls"}
	m.histIdx = 1
	m.input = "ls"
	m.cursorPos = 2
	mm, _ := m.handleEnter()
	got := mm.(tuiModel)
	if len(got.history) != 1 || got.history[0] != "ls" {
		t.Fatalf("history = %v, want a single %q", got.history, "ls")
	}
	m2 := newTestTui()
	m2.input = "pwd"
	m2.cursorPos = 3
	mm2, _ := m2.handleEnter()
	got2 := mm2.(tuiModel)
	if len(got2.history) != 1 || got2.history[0] != "pwd" {
		t.Fatalf("history = %v, want a single %q", got2.history, "pwd")
	}
}

// /history показывает записи из файла вместе с текущей сессией, а не только
// последние 10 выполненных команд.
func TestSlashHistoryShowsFileAndSession(t *testing.T) {
	dir := t.TempDir()
	histFile := filepath.Join(dir, "history.jsonl")
	var sb strings.Builder
	for _, c := range []string{"file-1", "file-2"} {
		data, _ := json.Marshal(HistoryEntry{Timestamp: time.Now(), Command: c, Source: "direct"})
		sb.WriteString(string(data) + "\n")
	}
	if err := os.WriteFile(histFile, []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}

	rf := &rootFlags{cfg: config.Config{Mode: config.ModeAI}}
	s := &session{cfg: config.Config{Mode: config.ModeAI, HistoryFile: histFile}}
	m := NewTuiModel(rf, s).(tuiModel)
	s.addHistory("session-1", "direct")

	mm, _ := m.handleSlash("/history")
	got := mm.(tuiModel)
	for _, want := range []string{"file-1", "file-2", "session-1"} {
		if !strings.Contains(got.content, want) {
			t.Fatalf("/history should list %q, content:\n%s", want, got.content)
		}
	}
}

// Модель может сколько угодно возвращать неработающую команду. Без
// ограничения каждая попытка — новый запрос и новый запуск, то есть цикл
// намертво зависает на «thinking…».
func TestAutoCorrectStopsAfterLimit(t *testing.T) {
	m := newTestTui()
	m.width = 100
	m.height = 20
	m.s.engine = &captureEngine{}
	resp := prompt.Response{
		Intent:  prompt.IntentRunCommand,
		Command: "exit 3",
		Risk:    prompt.RiskLow,
	}
	fail := executor.Result{ExitCode: 3, Err: context.DeadlineExceeded}

	attempts := 0
	for {
		attempts++
		if attempts > maxAutoFix+3 {
			t.Fatalf("цикл не остановился: лимит %d, а запросов %d", maxAutoFix, attempts)
		}
		mm, cmd := m.autoCorrect(resp, fail)
		m = mm
		if cmd == nil {
			break
		}
		if m.state != tuiStreaming {
			break
		}
	}

	if attempts != maxAutoFix+1 {
		t.Fatalf("запросов на исправление = %d, want %d (%d попыток + одно сообщение о сдаче)",
			attempts, maxAutoFix+1, maxAutoFix)
	}
	if m.state != tuiIdle {
		t.Fatalf("после сдачи state = %d, want tuiIdle", m.state)
	}
	if !strings.Contains(m.content, "giving up on auto-fix") {
		t.Fatalf("пользователь должен знать, что автокоррекция исчерпана:\n%s", m.content)
	}
}

// pumpCmd доводит фоновую команду до конца. Она отдаёт куски живого вывода и
// только потом итог, поэтому одного вызова cmd() мало: итог — последнее
// сообщение, а не первое.
