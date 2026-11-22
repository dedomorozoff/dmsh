package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

func TestWebSearchToolUsesProviderSearchModel(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Go 1.25 released — https://go.dev/blog — new features"}}]}`))
	}))
	defer srv.Close()

	s := newToolSession(t, &toolFakeEngine{})
	s.cfg.RemoteBaseURL = srv.URL
	s.cfg.SearchModel = "gemini-search"

	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{"query":"latest go version","max_results":3}`}, io.Discard))
	if !res.OK || !strings.Contains(res.Content, "go.dev/blog") {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(string(gotBody), `"model":"gemini-search"`) {
		t.Fatalf("search model not used: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), "latest go version") || !strings.Contains(string(gotBody), "at most 3 results") {
		t.Fatalf("query not forwarded: %s", gotBody)
	}
	if s.searchEngine == nil {
		t.Fatal("search engine must be created lazily and kept on the session")
	}
	s.close()
	if s.searchEngine != nil {
		t.Fatal("close must release the search engine")
	}
}

func TestWebSearchToolRefusesLocalProvider(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	s.provider = config.ProviderLocal
	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{"query":"news"}`}, io.Discard))
	if res.OK || !strings.Contains(res.Error, "Pollinations") {
		t.Fatalf("result = %+v, want a refusal naming Pollinations", res)
	}
}

// Поисковая модель живёт только у Pollinations: локальный GGUF и локальный
// Ollama-сервер в интернет не ходят.
func TestWebSearchToolRefusesOllama(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	s.provider = config.ProviderOllama
	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{"query":"news"}`}, io.Discard))
	if res.OK || !strings.Contains(res.Error, "Pollinations") {
		t.Fatalf("result = %+v, want a refusal naming Pollinations", res)
	}
}

func TestWebSearchToolRejectsBadArguments(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	if res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{"query":"  "}`}, io.Discard)); res.OK {
		t.Fatal("blank query must fail")
	}
	if res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{`}, io.Discard)); res.OK {
		t.Fatal("broken arguments must fail")
	}
}

func TestWebSearchPropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := newToolSession(t, &toolFakeEngine{})
	s.cfg.RemoteBaseURL = srv.URL
	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameWebSearch, Arguments: `{"query":"news"}`}, io.Discard))
	if res.OK || !strings.Contains(res.Error, "search failed") {
		t.Fatalf("result = %+v, want a search failure", res)
	}
}

func TestTodoToolThroughSession(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	ctx := context.Background()

	if res := decodeToolResult(t, s.tools.Call(ctx, tools.NameTodo, `{"action":"add","task":"inspect service"}`)); !res.OK {
		t.Fatalf("add = %+v", res)
	}
	if got := len(s.todos.Items()); got != 1 {
		t.Fatalf("todo items = %d, want 1", got)
	}
	if res := decodeToolResult(t, s.tools.Call(ctx, tools.NameTodo, `{"action":"done","id":1}`)); !res.OK {
		t.Fatalf("done = %+v", res)
	}

	var out strings.Builder
	showTodo(&out, s)
	if !strings.Contains(out.String(), "inspect service") || !strings.Contains(out.String(), "done") {
		t.Fatalf("/todo output = %q", out.String())
	}
	handleTodo("clear", io.Discard, s)
	if len(s.todos.Items()) != 0 {
		t.Fatal("/todo clear must empty the list")
	}
	handleTodo("bogus", io.Discard, s)
}

func TestShowTodoWithoutStore(t *testing.T) {
	s := &session{}
	var out strings.Builder
	showTodo(&out, s)
	if !strings.Contains(out.String(), "not available") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestToolLoopAsksUserThenFinishes(t *testing.T) {
	eng := &toolFakeEngine{replies: []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "a1", Type: "function", Name: tools.NameTodo, Arguments: `{"action":"add","task":"check disk"}`},
			{ID: "a2", Type: "function", Name: tools.NameAsk, Arguments: `{"question":"which mount point?"}`},
		}},
		{Role: llm.RoleAssistant, Content: `{"intent":"explain","explanation":"checked /var"}`},
	}}
	s := newToolSession(t, eng)
	s.SetInput(&fixedReader{lines: []string{"/var"}})

	var out strings.Builder
	resp, _, err := s.askStream(context.Background(), "run", "check the disk", &out)
	if err != nil {
		t.Fatalf("askStream: %v", err)
	}
	if resp.Intent != prompt.IntentExplain {
		t.Fatalf("intent = %q", resp.Intent)
	}
	if len(eng.calls) != 2 {
		t.Fatalf("engine calls = %d, want 2", len(eng.calls))
	}

	// Оба вызова должны уйти в одном сообщении, а их результаты — в двух
	// последующих tool-сообщениях.
	toolMsgs := eng.calls[1].Messages[2:]
	if len(toolMsgs) != 3 {
		t.Fatalf("messages after assistant = %d, want assistant+2 tool results", len(toolMsgs)+1)
	}
	if len(toolMsgs[0].ToolCalls) != 2 {
		t.Fatalf("assistant must carry both tool calls: %+v", toolMsgs[0])
	}
	if !strings.Contains(toolMsgs[1].Content, "check disk") {
		t.Fatalf("todo result = %s", toolMsgs[1].Content)
	}
	if !strings.Contains(toolMsgs[2].Content, "/var") {
		t.Fatalf("ask result = %s", toolMsgs[2].Content)
	}
	if !strings.Contains(out.String(), "/var") {
		t.Fatalf("answer should be echoed back: %q", out.String())
	}
	if len(s.todos.Items()) != 1 {
		t.Fatalf("todo list = %+v", s.todos.Items())
	}
}

// Пользователь должен видеть результат todo: раньше в транскрипте оставалась
// только пустая строка и голая метка [tool], а список уходил одной модели.
func TestToolLoopShowsTodoResultToUser(t *testing.T) {
	eng := &toolFakeEngine{replies: []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "t1", Type: "function", Name: tools.NameTodo, Arguments: `{"action":"add","task":"check disk"}`},
		}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "t2", Type: "function", Name: tools.NameTodo, Arguments: `{"action":"list"}`},
		}},
		{Role: llm.RoleAssistant, Content: `{"intent":"explain","explanation":"done","command":"df -h"}`},
	}}
	s := newToolSession(t, eng)
	tc, ok := s.toolEngine()
	if !ok {
		t.Fatal("tools must be enabled for this test")
	}

	var out strings.Builder
	if err := s.chatWithTools(context.Background(), tc, "sys", "user",
		llm.SamplingOptions{}, newStreamPrinter(&out), &out); err != nil {
		t.Fatalf("chatWithTools: %v", err)
	}
	text := out.String()
	for _, want := range []string{"added [1] check disk", "open [1] check disk"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the todo result must be visible to the user, missing %q:\n%q", want, text)
		}
	}
}

func TestPromptToolNamesOnlyWhenEngineSupportsTools(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	names := s.promptToolNames()
	if len(names) != 7 {
		t.Fatalf("tool names = %v, want all 7 tools", names)
	}
	s.cfg.ToolsEnabled = false
	if names := s.promptToolNames(); names != nil {
		t.Fatalf("tools disabled must not be advertised: %v", names)
	}
}

// В help-режиме модель не должна даже видеть run_command: режим учит и
// ничего не выполняет.
func TestHelpModeHidesRunCommandTool(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	advertised := false
	for _, spec := range s.toolSpecs() {
		if spec.Name == tools.NameRunCommand {
			advertised = true
		}
	}
	if !advertised {
		t.Fatal("ai mode must advertise run_command")
	}

	s.cfg.Mode = config.ModeHelp
	for _, spec := range s.toolSpecs() {
		if spec.Name == tools.NameRunCommand {
			t.Fatal("run_command must not be advertised in help mode")
		}
	}
	names := s.promptToolNames()
	for _, n := range names {
		if n == tools.NameRunCommand {
			t.Fatalf("system prompt must not mention run_command in help mode: %v", names)
		}
	}
	// Остальные инструменты остаются: читать файлы и искать в вебе полезно.
	if len(names) != 6 {
		t.Fatalf("help mode tool names = %v, want 6 (all but run_command)", names)
	}
}

// Режим могут переключить между шагами диалога, поэтому проверка повторяется
// на самом вызове инструмента.
func TestHelpModeRefusesRunCommandCall(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	s.setAutoYes(true)
	s.cfg.Mode = config.ModeHelp

	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameRunCommand, Arguments: `{"command":"echo dmsh-should-not-run"}`}, io.Discard))
	if res.OK {
		t.Fatal("run_command must fail in help mode")
	}
	if !strings.Contains(res.Error, "help mode") {
		t.Fatalf("error should name the reason, got %q", res.Error)
	}
	if res.Content != "" {
		t.Fatalf("nothing should have run, got output %q", res.Content)
	}
}

func TestHelpModeRefusesRunCommandThroughEngine(t *testing.T) {
	eng := &toolFakeEngine{replies: []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "c1", Type: "function", Name: tools.NameRunCommand, Arguments: `{"command":"echo dmsh-should-not-run"}`},
		}},
		{Role: llm.RoleAssistant, Content: `{"intent":"explain","explanation":"listed"}`},
	}}
	s := newToolSession(t, eng)
	s.cfg.Mode = config.ModeHelp
	s.setAutoYes(true)

	var out strings.Builder
	if _, _, err := s.askStream(context.Background(), "run", "list files", &out); err != nil {
		t.Fatalf("askStream: %v", err)
	}
	if len(eng.calls) == 0 {
		t.Fatal("engine was never called")
	}
	for _, spec := range eng.calls[0].Tools {
		if spec.Name == tools.NameRunCommand {
			t.Fatal("run_command reached the model in help mode")
		}
	}
	for _, msg := range eng.calls[1].Messages {
		if strings.Contains(msg.Content, "dmsh-should-not-run") {
			t.Fatalf("the command must not have run: %q", msg.Content)
		}
	}
}

// В one-shot с пайпом stdin уходит в контекст, поэтому инструмент ask
// модели не предлагается: задать вопрос уже некому.
func TestAskUnregisteredWhenStdinPiped(t *testing.T) {
	s := newToolSession(t, &toolFakeEngine{})
	s.tools.Unregister(tools.NameAsk)

	names := s.promptToolNames()
	for _, n := range names {
		if n == tools.NameAsk {
			t.Fatalf("ask must not be advertised: %v", names)
		}
	}
	res := decodeToolResult(t, s.runToolCall(context.Background(),
		llm.ToolCall{Name: tools.NameAsk, Arguments: `{"question":"q"}`}, io.Discard))
	if res.OK {
		t.Fatal("unregistered ask must not succeed")
	}
}
