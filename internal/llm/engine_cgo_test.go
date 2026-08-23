//go:build llama

package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSmokeGenerate — быстрый дым-тест реальной модели. Запускается только
// если задан DMSH_TEST_MODEL=/path/to/model.gguf и собрано с -tags llama.
func TestSmokeGenerate(t *testing.T) {
	model := os.Getenv("DMSH_TEST_MODEL")
	if model == "" {
		t.Skip("DMSH_TEST_MODEL не задан")
	}

	eng, err := New(Params{
		ModelPath: model,
		Threads:   4,
		CtxSize:   2048,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := eng.Generate(ctx, "You are a helpful assistant.", "Reply with the single word: pong",
		SamplingOptions{MaxTokens: 16, Temperature: 0})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "pong") {
		t.Fatalf("expected 'pong' in output, got %q", out)
	}
}
