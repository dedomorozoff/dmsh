package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewUnknownProvider(t *testing.T) {
	_, err := New(Params{Provider: Provider("openrouter")})
	if err == nil {
		t.Fatal("unknown provider must fail")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error should explain the problem, got %v", err)
	}
}

func TestNewLocalWithoutPath(t *testing.T) {
	for _, p := range []Provider{ProviderLocal, ProviderAuto, ""} {
		if _, err := New(Params{Provider: p}); !errors.Is(err, ErrNoLocalModel) {
			t.Fatalf("provider %q: err = %v, want ErrNoLocalModel", p, err)
		}
	}
}

func TestNewPollinationsSupportsTools(t *testing.T) {
	eng, err := New(Params{Provider: ProviderPollinations, RemoteModel: "openai"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !SupportsTools(eng) {
		t.Fatal("pollinations engine must support tools")
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSupportsToolsNil(t *testing.T) {
	if SupportsTools(nil) {
		t.Fatal("nil engine has no tools")
	}
}

func TestProviderDefaultsToAutoInNew(t *testing.T) {
	// Auto без пути к модели — это ошибка, а не молчаливый выбор удалённого
	// провайдера: выбор делает слой CLI один раз при старте сессии.
	if _, err := New(Params{}); !errors.Is(err, ErrNoLocalModel) {
		t.Fatalf("err = %v, want ErrNoLocalModel", err)
	}
}

func TestPollinationsGenerateContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	eng, _ := New(Params{Provider: ProviderPollinations, RemoteBaseURL: "http://127.0.0.1:1"})
	if _, err := eng.Generate(ctx, "", "hi", SamplingOptions{}); err == nil {
		t.Fatal("cancelled context must abort the request")
	}
}
