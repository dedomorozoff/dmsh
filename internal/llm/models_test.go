package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Каталог Ollama: id с тегом, отсортированный по имени.
func TestListRemoteModelsOllama(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"qwen3:8b","object":"model","owned_by":"library"},
			{"id":"deepcoder:1.5b","object":"model","owned_by":"local"}]}`))
	}))
	defer srv.Close()

	got, err := ListRemoteModels(Params{Provider: ProviderOllama, RemoteBaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatalf("ListRemoteModels: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if len(got) != 2 || got[0].ID != "deepcoder:1.5b" || got[1].ID != "qwen3:8b" {
		t.Fatalf("catalog = %+v, want two models sorted by id", got)
	}
	if got[0].Note != "local" {
		t.Fatalf("note = %q, want the owner from the response", got[0].Note)
	}
}

// У Pollinations идентификаторы с publisher и слагом.
func TestListRemoteModelsPollinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5.4-nano"},{"id":"mistral-medium"}]}`))
	}))
	defer srv.Close()

	got, err := ListRemoteModels(Params{Provider: ProviderPollinations, RemoteBaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatalf("ListRemoteModels: %v", err)
	}
	if len(got) != 2 || got[0].ID != "mistral-medium" || got[1].ID != "openai/gpt-5.4-nano" {
		t.Fatalf("catalog = %+v", got)
	}
}

// Каталог не требует ключа: в запросе не должно быть заголовка с секретом.
func TestListRemoteModelsSendsNoAuthorization(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:8b"}]}`))
	}))
	defer srv.Close()

	_, err := ListRemoteModels(Params{
		Provider:      ProviderPollinations,
		RemoteBaseURL: srv.URL + "/v1",
		APIKey:        "sk-secret",
	})
	if err != nil {
		t.Fatalf("ListRemoteModels: %v", err)
	}
	if present {
		t.Fatal("the model list must not carry the api key")
	}
}

func TestListRemoteModelsLocalProviderHasNoCatalog(t *testing.T) {
	if _, err := ListRemoteModels(Params{Provider: ProviderLocal}); err == nil {
		t.Fatal("a local gguf has no remote catalog")
	}
}

// Ответ без моделей — это пустой список, а не ошибка: у провайдера просто
// пока ничего не загружено.
func TestListRemoteModelsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	got, err := ListRemoteModels(Params{Provider: ProviderOllama, RemoteBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("ListRemoteModels: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("catalog = %+v, want empty", got)
	}
}

func TestListRemoteModelsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListRemoteModels(Params{Provider: ProviderPollinations, RemoteBaseURL: srv.URL})
	if err == nil {
		t.Fatal("an error response must be reported")
	}
	if !strings.Contains(err.Error(), "model list") {
		t.Fatalf("error should say what failed: %v", err)
	}
}

// Ошибка соединения с Ollama почти всегда означает «сервер не запущен».
func TestListRemoteModelsUnreachableOllama(t *testing.T) {
	// Порт 1 не слушается ничем: соединение отклоняется сразу.
	_, err := ListRemoteModels(Params{Provider: ProviderOllama, RemoteBaseURL: "http://127.0.0.1:1/v1"})
	if err == nil {
		t.Fatal("an unreachable server must be reported")
	}
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("error should suggest starting the server: %v", err)
	}
}
