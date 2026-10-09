package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer отдаёт заранее заданные SSE-строки и проверяет тело запроса.
func sseServer(t *testing.T, chunks []string, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			data, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Errorf("request body is not JSON: %v (%s)", err, data)
			}
			*capture = parsed
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

func TestNewPollinationsDefaults(t *testing.T) {
	eng, err := NewPollinations(Params{RemoteBaseURL: "https://example.com/openai/", RemoteModel: "  mistral  "})
	if err != nil {
		t.Fatalf("NewPollinations: %v", err)
	}
	p := eng.(*pollinationsEngine)
	if p.baseURL != "https://example.com/openai" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", p.baseURL)
	}
	if p.model != "mistral" {
		t.Fatalf("model = %q, want %q", p.model, "mistral")
	}
	if p.client.Timeout != DefaultRemoteTimeout {
		t.Fatalf("timeout = %v, want %v", p.client.Timeout, DefaultRemoteTimeout)
	}

	def, err := NewPollinations(Params{})
	if err != nil {
		t.Fatalf("NewPollinations(empty): %v", err)
	}
	d := def.(*pollinationsEngine)
	if d.baseURL != DefaultPollinationsBaseURL || d.model != DefaultPollinationsModel {
		t.Fatalf("defaults = %q/%q, want %q/%q", d.baseURL, d.model, DefaultPollinationsBaseURL, DefaultPollinationsModel)
	}
}

func TestPollinationsGenerate(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("anonymous access must not send Authorization header, got %q", auth)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer srv.Close()

	eng, err := NewPollinations(Params{RemoteBaseURL: srv.URL, RemoteModel: "openai"})
	if err != nil {
		t.Fatalf("NewPollinations: %v", err)
	}
	out, err := eng.Generate(context.Background(), "sys", "usr", SamplingOptions{MaxTokens: 64, Temperature: 0.2})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "hello" {
		t.Fatalf("content = %q, want %q", out, "hello")
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v, want system+user", got["messages"])
	}
	if got["model"] != "openai" {
		t.Fatalf("model = %v, want openai", got["model"])
	}
	if got["max_tokens"] != float64(64) {
		t.Fatalf("max_tokens = %v, want 64", got["max_tokens"])
	}
}

func TestPollinationsStream(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"content":"{\"intent\":"}}]}`,
		`data: {"choices":[{"delta":{"content":"\"explain\""}}]}`,
		"data: [DONE]",
	}, nil)
	defer srv.Close()

	eng, err := NewPollinations(Params{RemoteBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewPollinations: %v", err)
	}
	tokens := make(chan string, 16)
	if err := eng.Stream(context.Background(), "", "hi", SamplingOptions{}, tokens); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sb strings.Builder
	for tok := range tokens {
		sb.WriteString(tok)
	}
	if sb.String() != `{"intent":"explain"` {
		t.Fatalf("streamed = %q", sb.String())
	}
}

func TestPollinationsStreamSkipsBrokenChunk(t *testing.T) {
	srv := sseServer(t, []string{
		": keep-alive",
		"data: not-json",
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		"data: [DONE]",
	}, nil)
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	tokens := make(chan string, 16)
	if err := eng.Stream(context.Background(), "", "hi", SamplingOptions{}, tokens); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sb strings.Builder
	for tok := range tokens {
		sb.WriteString(tok)
	}
	if sb.String() != "ok" {
		t.Fatalf("streamed = %q, want %q", sb.String(), "ok")
	}
}

func TestPollinationsStreamAlwaysClosesChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	tokens := make(chan string, 4)
	err := eng.Stream(context.Background(), "", "hi", SamplingOptions{}, tokens)
	if err == nil {
		t.Fatal("HTTP 429 must be reported as error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("error should mention status, got %v", err)
	}
	// Канал обязан быть закрыт, иначе потребитель зависнет.
	for range tokens {
	}
}

func TestPollinationsChatSendsNestedTools(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer srv.Close()

	eng, err := NewPollinations(Params{RemoteBaseURL: srv.URL, RemoteModel: "openai"})
	if err != nil {
		t.Fatalf("NewPollinations: %v", err)
	}
	msg, err := eng.(ToolEngine).Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []ToolSpec{{Name: "read_file", Description: "read", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content != "done" || msg.Role != RoleAssistant {
		t.Fatalf("message = %+v", msg)
	}
	if got["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %v, want auto", got["tool_choice"])
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v, want one entry", got["tools"])
	}
	first, _ := tools[0].(map[string]any)
	if first["type"] != "function" {
		t.Fatalf("tool type = %v, want function", first["type"])
	}
	fn, ok := first["function"].(map[string]any)
	if !ok || fn["name"] != "read_file" {
		t.Fatalf("tools[0].function = %v", first["function"])
	}
}

func TestPollinationsChatAssemblesToolCallDeltas(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"run_command","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"comm"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"and\":\"pwd\"}"}}]}}]}`,
		"data: [DONE]",
	}, nil)
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	deltas := make(chan string, 8)
	msg, err := eng.(ToolEngine).Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "list files"}},
		Tools:    []ToolSpec{{Name: "run_command"}},
		Deltas:   deltas,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	for range deltas {
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v, want 1", msg.ToolCalls)
	}
	call := msg.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "run_command" {
		t.Fatalf("call = %+v", call)
	}
	var args RunCommandArgsForTest
	if err := call.DecodeArgs(&args); err != nil {
		t.Fatalf("DecodeArgs: %v", err)
	}
	if args.Command != "pwd" {
		t.Fatalf("decoded command = %q, want %q", args.Command, "pwd")
	}
}

// RunCommandArgsForTest — локальный аналог аргументов run_command, чтобы
// тест не тянул в себя пакет tools.
type RunCommandArgsForTest struct {
	Command string `json:"command"`
}

func TestPollinationsChatWholeMessageChunk(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"choices":[{"message":{"role":"assistant","content":"all at once","tool_calls":[{"id":"c1","type":"function","name":"list_dir","arguments":"{}"}]},"finish_reason":"tool_calls"}]}`,
		"data: [DONE]",
	}, nil)
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	deltas := make(chan string, 8)
	msg, err := eng.(ToolEngine).Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Deltas:   deltas,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	for range deltas {
	}
	if msg.Content != "all at once" {
		t.Fatalf("content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "list_dir" {
		t.Fatalf("tool calls = %+v", msg.ToolCalls)
	}
}

func TestPollinationsChatRejectsEmptyMessages(t *testing.T) {
	eng, _ := NewPollinations(Params{RemoteBaseURL: "http://127.0.0.1:1"})
	if _, err := eng.(ToolEngine).Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("empty chat request must fail without touching the network")
	}
}

func TestPollinationsAPIErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	if _, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{}); err == nil {
		t.Fatal("error response must be reported")
	} else if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error should surface API message, got %v", err)
	}
}

func TestPollinationsRateLimitHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	_, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{})
	if err == nil {
		t.Fatal("429 must be an error")
	}
	if !strings.Contains(err.Error(), "one request at a time") {
		t.Fatalf("error should explain the anonymous limit: %v", err)
	}
}

func TestPollinationsTokenHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	_, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{})
	if err == nil {
		t.Fatal("402 must be an error")
	}
	if !strings.Contains(err.Error(), "--remote-base-url") {
		t.Fatalf("error should point to an alternative: %v", err)
	}
}

func TestToolCallDecodeArgsEmpty(t *testing.T) {
	var args RunCommandArgsForTest
	if err := (ToolCall{Name: "run_command"}).DecodeArgs(&args); err != nil {
		t.Fatalf("empty arguments should decode to zero value: %v", err)
	}
	if args.Command != "" {
		t.Fatalf("command = %q, want empty", args.Command)
	}
}

func TestNewToolResult(t *testing.T) {
	msg := NewToolResult(ToolCall{ID: "c1", Name: "read_file"}, `{"ok":true}`)
	if msg.Role != RoleTool || msg.ToolCallID != "c1" || msg.Name != "read_file" {
		t.Fatalf("message = %+v", msg)
	}
}

// Вызов инструмента уходит обратно в API вложенным объектом function —
// иначе следующий запрос в диалоге будет отвергнут сервером.
func TestMessageWithToolCallsMarshalsNestedFunction(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Name: "run_command", Arguments: `{"command":"pwd"}`},
		},
	}
	data, err := json.Marshal([]Message{msg, NewToolResult(msg.ToolCalls[0], `{"ok":true}`)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	calls, _ := decoded[0]["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %v", decoded[0]["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	fn, ok := call["function"].(map[string]any)
	if !ok {
		t.Fatalf("tool call must nest function object, got %v", call)
	}
	if fn["name"] != "run_command" || fn["arguments"] != `{"command":"pwd"}` {
		t.Fatalf("function = %v", fn)
	}
	if decoded[1]["role"] != "tool" || decoded[1]["tool_call_id"] != "c1" {
		t.Fatalf("tool result message = %v", decoded[1])
	}
}

func TestToolCallUnmarshalAcceptsFlatForm(t *testing.T) {
	var call ToolCall
	if err := json.Unmarshal([]byte(`{"id":"c2","name":"list_dir","arguments":"{}"}`), &call); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if call.ID != "c2" || call.Name != "list_dir" || call.Arguments != "{}" {
		t.Fatalf("call = %+v", call)
	}
}
