//go:build !llama

package llm

import "context"

// stubEngine — заглушка, используется в сборках без `-tags llama`.
// Полезна для разработки, тестов парсинга и CI без C-тулчейна.
type stubEngine struct{}

// newLocalEngine возвращает stub-движок. Параметры игнорируются.
func newLocalEngine(_ Params) (Engine, error) {
	return &stubEngine{}, nil
}

func (*stubEngine) Generate(_ context.Context, _, _ string, _ SamplingOptions) (string, error) {
	return "", ErrNotBuiltWithCGO
}

func (*stubEngine) Stream(_ context.Context, _, _ string, _ SamplingOptions, tokens chan<- string) error {
	close(tokens)
	return ErrNotBuiltWithCGO
}

func (*stubEngine) Close() error { return nil }
