package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOllamaDefaults(t *testing.T) {
	eng, err := NewOllama(Params{})
	if err != nil {
		t.Fatalf("NewOllama: %v", err)
	}
	e := eng.(*openaiEngine)
	if e.baseURL != DefaultOllamaBaseURL {
		t.Fatalf("baseURL = %q, want %q", e.baseURL, DefaultOllamaBaseURL)
	}
	if e.model != DefaultOllamaModel {
		t.Fatalf("model = %q, want %q", e.model, DefaultOllamaModel)
	}
	if e.name != remoteProviderOllama {
		t.Fatalf("name = %q, want %q (it shows up in error messages)", e.name, remoteProviderOllama)
	}

	custom, err := NewOllama(Params{RemoteBaseURL: "http://10.0.0.5:11434/v1/", RemoteModel: "  llama3.2  "})
	if err != nil {
		t.Fatalf("NewOllama(custom): %v", err)
	}
	c := custom.(*openaiEngine)
	if c.baseURL != "http://10.0.0.5:11434/v1" || c.model != "llama3.2" {
		t.Fatalf("custom = %q/%q", c.baseURL, c.model)
	}
}

// Ключ Pollinations не должен утекать в локальный сервер, даже если он
// задан в переменной окружения.
func TestOllamaNeverSendsAuthorization(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	eng, err := NewOllama(Params{RemoteBaseURL: srv.URL, APIKey: "sk-leaked"})
	if err != nil {
		t.Fatalf("NewOllama: %v", err)
	}
	if _, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if present {
		t.Fatal("ollama must not receive an Authorization header")
	}
}

func TestOllamaGenerate(t *testing.T) {
	var gotModel, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotModel = string(buf[:n])
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi there"}}]}`))
	}))
	defer srv.Close()

	eng, err := NewOllama(Params{RemoteBaseURL: srv.URL + "/v1", RemoteModel: "qwen2.5-coder:7b"})
	if err != nil {
		t.Fatalf("NewOllama: %v", err)
	}
	out, err := eng.Generate(context.Background(), "sys", "hi", SamplingOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "hi there" {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(gotModel, `"model":"qwen2.5-coder:7b"`) {
		t.Fatalf("request body should carry the model, got %q", gotModel)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
}

// Модель, которой нет на сервере, отвечает 404: подсказка должна говорить,
// что модель нужно скачать, а не менять провайдера.
func TestOllamaMissingModelHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"model \"x\" not found"}}`))
	}))
	defer srv.Close()

	eng, _ := NewOllama(Params{RemoteBaseURL: srv.URL})
	_, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{})
	if err == nil {
		t.Fatal("404 must be an error")
	}
	if !strings.Contains(err.Error(), "ollama pull") {
		t.Fatalf("error should suggest pulling the model: %v", err)
	}
}

// Ошибки Ollama называют провайдера: «ollama HTTP 404» и «pollinations
// HTTP 402» — разные ситуации с разными действиями.
func TestRemoteErrorMessagesNameTheProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	eng, _ := NewPollinations(Params{RemoteBaseURL: srv.URL})
	_, err := eng.Generate(context.Background(), "", "hi", SamplingOptions{})
	if err == nil || !strings.Contains(err.Error(), "pollinations HTTP 402") {
		t.Fatalf("error = %v, want a pollinations 402", err)
	}

	local, _ := NewOllama(Params{RemoteBaseURL: srv.URL})
	if _, err := local.Generate(context.Background(), "", "hi", SamplingOptions{}); err == nil ||
		!strings.Contains(err.Error(), "ollama HTTP 402") {
		t.Fatalf("error = %v, want an ollama 402", err)
	}
}

func TestNewDispatchesOllama(t *testing.T) {
	eng, err := New(Params{Provider: ProviderOllama, RemoteBaseURL: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
	}
	if _, ok := eng.(*openaiEngine); !ok {
		t.Fatalf("New(ollama) = %T, want the shared OpenAI-compatible engine", eng)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
