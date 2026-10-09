//go:build !llama

package llm

import "testing"

// В сборке без CGO локальный провайдер возвращает заглушку: она не умеет
// ничего, но должна удовлетворять интерфейсу Engine.
func TestNewLocalReturnsStubEngine(t *testing.T) {
	eng, err := New(Params{Provider: ProviderLocal, ModelPath: "model.gguf", RemoteModel: "mistral"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := eng.(*stubEngine); !ok {
		t.Fatalf("stub build must return stubEngine, got %T", eng)
	}
	if SupportsTools(eng) {
		t.Fatal("stub engine must not advertise tool support")
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
