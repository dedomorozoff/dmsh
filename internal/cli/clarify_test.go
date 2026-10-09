package cli

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// clarifyingEngine отвечает заранее заданной последовательностью: строки
// для Stream и Generate расходуются независимо, последняя повторяется.
type clarifyingEngine struct {
	answers     []string
	streamPos   int
	streamCalls int
	genPos      int
	lastInput   string
}

func (e *clarifyingEngine) Generate(_ context.Context, _, user string, _ llm.SamplingOptions) (string, error) {
	e.lastInput = user
	idx := e.genPos
	if idx >= len(e.answers) {
		idx = len(e.answers) - 1
	}
	e.genPos++
	return e.answers[idx], nil
}

func (e *clarifyingEngine) Stream(_ context.Context, _, user string, _ llm.SamplingOptions, out chan<- string) error {
	defer close(out)
	e.lastInput = user
	e.streamCalls++
	idx := e.streamPos
	if idx >= len(e.answers) {
		idx = len(e.answers) - 1
	}
	e.streamPos++
	out <- e.answers[idx]
	return nil
}

func (*clarifyingEngine) Close() error { return nil }

func newClarifySession(t *testing.T, eng llm.Engine, input LineReader) *session {
	t.Helper()
	cfg := config.Config{
		Mode:         config.ModeAI,
		Shell:        testShell(),
		Provider:     config.ProviderPollinations,
		RemoteModel:  "openai",
		ToolsEnabled: true,
		MaxTokens:    128,
		Temperature:  0.1,
		TopP:         0.9,
	}
	s := &session{
		cfg:      cfg,
		provider: config.ProviderPollinations,
		engine:   eng,
		tools:    newToolRegistry(cfg.Shell),
		todos:    tools.NewTodoStore(),
	}
	s.registerOptionalTools()
	if input != nil {
		s.SetInput(input)
	}
	return s
}

const laravelRequest = "браток, сделай пустой проект ларавель"

const laravelQuestion = `{"intent":"ask_clarification","question":"Какое имя проекта Laravel вы хотите создать?"}`

func TestClarifyLoopFeedsAnswerBackToModel(t *testing.T) {
	eng := &clarifyingEngine{answers: []string{
		laravelQuestion,
		`{"intent":"run_command","command":"composer create-project laravel/laravel shop","explanation":"создаём проект","risk_level":"medium"}`,
	}}
	s := newClarifySession(t, eng, &fixedReader{lines: []string{"1. shop"}})

	var out strings.Builder
	resp, err := askWithFollowUp(context.Background(), s, "run", laravelRequest, &out, &out)
	if err != nil {
		t.Fatalf("askWithFollowUp: %v", err)
	}
	if resp.Intent != prompt.IntentRunCommand || resp.Command != "composer create-project laravel/laravel shop" {
		t.Fatalf("response = %+v", resp)
	}

	// Второй запрос обязан содержать и вопрос, и ответ пользователя.
	sent := eng.lastInput
	if !strings.Contains(sent, "Какое имя проекта Laravel") {
		t.Fatalf("follow-up lost the question:\n%s", sent)
	}
	if !strings.Contains(sent, "shop") {
		t.Fatalf("follow-up lost the answer:\n%s", sent)
	}
	if strings.Contains(sent, "1. shop") {
		t.Fatalf("list marker must be stripped:\n%s", sent)
	}
	if !strings.Contains(out.String(), "Какое имя проекта") {
		t.Fatalf("question should be shown: %q", out.String())
	}
}

func TestClarifyLoopStopsWhenModelRepeatsQuestion(t *testing.T) {
	eng := &clarifyingEngine{answers: []string{laravelQuestion}}
	s := newClarifySession(t, eng, &fixedReader{lines: []string{"shop"}})

	var out strings.Builder
	resp, err := askWithFollowUp(context.Background(), s, "run", laravelRequest, &out, &out)
	if err != nil {
		t.Fatalf("askWithFollowUp: %v", err)
	}
	if resp.Intent != prompt.IntentAskClarification {
		t.Fatalf("response = %+v", resp.Intent)
	}
	if eng.streamCalls != 2 {
		t.Fatalf("model calls = %d, want 2: the repeat must not be asked again", eng.streamCalls)
	}
	if !strings.Contains(out.String(), "rephrase the request") {
		t.Fatalf("user should be told why dmsh stopped: %q", out.String())
	}
}

func TestClarifyLoopStopsAfterMaxRounds(t *testing.T) {
	// Каждый раз новый вопрос: круги всё равно должны кончиться.
	eng := &clarifyingEngine{answers: []string{
		`{"intent":"ask_clarification","question":"Какое имя?"}`,
		`{"intent":"ask_clarification","question":"Какая версия PHP?"}`,
		`{"intent":"ask_clarification","question":"А какая БД?"}`,
		`{"intent":"ask_clarification","question":"Ещё что-нибудь?"}`,
	}}
	s := newClarifySession(t, eng, &fixedReader{lines: []string{"a", "b", "c", "d"}})

	var out strings.Builder
	if _, err := askWithFollowUp(context.Background(), s, "run", laravelRequest, &out, &out); err != nil {
		t.Fatalf("askWithFollowUp: %v", err)
	}
	if got := eng.streamCalls; got > maxClarifyRounds+1 {
		t.Fatalf("model calls = %d, want at most %d", got, maxClarifyRounds+1)
	}
	if !strings.Contains(out.String(), "rephrase the request") {
		t.Fatalf("expected a stop notice, got: %q", out.String())
	}
}

func TestClarifyLoopLastRoundAsksForAnswer(t *testing.T) {
	eng := &clarifyingEngine{answers: []string{
		`{"intent":"ask_clarification","question":"Какое имя?"}`,
		`{"intent":"ask_clarification","question":"Какая БД?"}`,
		`{"intent":"run_command","command":"composer create-project laravel/laravel shop","risk_level":"low"}`,
	}}
	s := newClarifySession(t, eng, &fixedReader{lines: []string{"shop", "sqlite"}})

	var out strings.Builder
	if _, err := askWithFollowUp(context.Background(), s, "run", laravelRequest, &out, &out); err != nil {
		t.Fatalf("askWithFollowUp: %v", err)
	}
	if !strings.Contains(eng.lastInput, "last clarification") {
		t.Fatalf("final round should press for an answer:\n%s", eng.lastInput)
	}
}

func TestClarificationFollowUpAccumulatesTurns(t *testing.T) {
	var c clarification
	first := c.followUp("сделай проект", "какое имя?", "shop")
	if !strings.Contains(first, "какое имя?") || !strings.Contains(first, "shop") {
		t.Fatalf("first follow-up = %q", first)
	}
	second := c.followUp(first, "какая версия?", "11")
	if !strings.Contains(second, "какое имя?") || !strings.Contains(second, "какая версия?") {
		t.Fatalf("history must be preserved:\n%s", second)
	}
	if !strings.Contains(second, "[clarification 2/2]") {
		t.Fatalf("rounds must be numbered:\n%s", second)
	}
	if !strings.Contains(second, "last clarification") {
		t.Fatalf("second round should be the last:\n%s", second)
	}
	if !c.exhausted() {
		t.Fatal("store must be exhausted after maxClarifyRounds")
	}
}

func TestClarificationRepeatsLast(t *testing.T) {
	var c clarification
	if c.repeatsLast("любой вопрос") {
		t.Fatal("nothing was asked yet")
	}
	c.followUp("x", "какое имя?", "shop")
	if !c.repeatsLast("Какое имя?") {
		t.Fatal("case-insensitive repeat must be detected")
	}
	if c.repeatsLast("какая БД?") {
		t.Fatal("a different question is not a repeat")
	}
}

func TestTuiQuestionKeepsTranscriptAndForwardsAnswer(t *testing.T) {
	eng := &clarifyingEngine{answers: []string{laravelQuestion}}
	s := newClarifySession(t, eng, nil)
	m := NewTuiModel(&rootFlags{cfg: s.cfg}, s).(tuiModel)
	m = m.startedForTest()

	// Модель задала вопрос: он попадает в транскрипт, а не только в строку ввода.
	mm, _ := m.Update(streamDoneMsg{resp: prompt.Response{
		Intent:   prompt.IntentAskClarification,
		Question: laravelQuestionText(),
	}})
	m = mm.(tuiModel)
	if m.state != tuiQuestion {
		t.Fatalf("state = %v, want tuiQuestion", m.state)
	}
	if !strings.Contains(m.content, laravelQuestionText()) {
		t.Fatalf("question must stay in the transcript:\n%s", m.content)
	}

	// Пользователь отвечает — в транскрипт уходит только ответ.
	m.input = "shop"
	m.cursorPos = len(m.input)
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)
	if m.state != tuiStreaming {
		t.Fatalf("state = %v, want tuiStreaming", m.state)
	}
	if !strings.Contains(m.content, "> shop") {
		t.Fatalf("answer should be echoed:\n%s", m.content)
	}
	if strings.Contains(m.content, "[clarification 1/2]") {
		t.Fatalf("service framing must not be shown to the user:\n%s", m.content)
	}
	if len(m.clarify.turns) != 1 || m.clarify.turns[0].answer != "shop" {
		t.Fatalf("clarification state = %+v, want one answered turn", m.clarify.turns)
	}
}

func TestTuiStopsWhenModelRepeatsQuestion(t *testing.T) {
	eng := &clarifyingEngine{answers: []string{laravelQuestion}}
	s := newClarifySession(t, eng, nil)
	m := NewTuiModel(&rootFlags{cfg: s.cfg}, s).(tuiModel)
	m = m.startedForTest()

	q := prompt.Response{Intent: prompt.IntentAskClarification, Question: laravelQuestionText()}
	mm, _ := m.Update(streamDoneMsg{resp: q})
	m = mm.(tuiModel)

	m.input = "shop"
	m.cursorPos = len(m.input)
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(tuiModel)

	// Модель снова задало тот же вопрос — второй раз его не показываем.
	mm, _ = m.Update(streamDoneMsg{resp: q})
	m = mm.(tuiModel)
	if m.state == tuiQuestion {
		t.Fatal("repeated question must not be asked again")
	}
	if !strings.Contains(m.content, "rephrase the request") {
		t.Fatalf("expected a stop notice:\n%s", m.content)
	}
}

func laravelQuestionText() string {
	return "Какое имя проекта Laravel вы хотите создать?"
}

// startedForTest готовит модель к вводу: пустое поле и состояние idle.
func (m tuiModel) startedForTest() tuiModel {
	m.state = tuiIdle
	m.width = 80
	m.height = 24
	return m
}

func TestStripListMarker(t *testing.T) {
	cases := map[string]string{
		"1. shop":  "shop",
		"2) shop":  "shop",
		"10. a b":  "a b",
		"shop":     "shop",
		"":         "",
		"1.shop":   "1.shop",
		"1.  shop": "shop",
		"3.5":      "3.5",
		"v1.2.3":   "v1.2.3",
	}
	for in, want := range cases {
		if got := stripListMarker(in); got != want {
			t.Errorf("stripListMarker(%q) = %q, want %q", in, got, want)
		}
	}
}
