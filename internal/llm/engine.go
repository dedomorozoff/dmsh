package llm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Provider определяет, откуда берётся инференс.
type Provider string

const (
	// ProviderLocal — локальный GGUF через llama.cpp.
	ProviderLocal Provider = "local"
	// ProviderPollinations — удалённый OpenAI-совместимый endpoint Pollinations.
	ProviderPollinations Provider = "pollinations"
	// ProviderAuto — локальная модель, если путь задан, иначе Pollinations.
	// Выбор делается один раз при создании движка; при ошибке локального
	// инференса удалённый провайдер молча не подставляется.
	ProviderAuto Provider = "auto"
)

// ErrNotBuiltWithCGO возвращается stub-реализацией, когда бинарь собран
// без тега `llama` и реальный движок недоступен.
var ErrNotBuiltWithCGO = errors.New("llm: this build has no llama.cpp linked; rebuild with `-tags llama`")

// ErrNoLocalModel возвращается, когда выбран провайдер local, но GGUF не найден.
var ErrNoLocalModel = errors.New("llm: no local GGUF model found (run: dmsh model download)")

// Params — настройки загрузки модели и контекста.
type Params struct {
	// Provider по умолчанию ProviderLocal: пустое значение и ProviderAuto
	// требуют, чтобы вызовющая сторона уже разрешила путь к модели.
	Provider Provider
	// ModelPath используется только локальным провайдером.
	ModelPath string
	Threads   int
	CtxSize   int
	GPULayers int
	// RemoteModel и RemoteBaseURL — только для удалённых провайдеров.
	RemoteModel   string
	RemoteBaseURL string
	// Timeout ограничивает один запрос к удалённому API (0 = DefaultRemoteTimeout).
	Timeout time.Duration
}

// SamplingOptions — параметры генерации одного запроса.
type SamplingOptions struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	StopTokens  []string
	// Seed=0 -> случайный.
	Seed uint32
}

// Engine — обобщённый интерфейс инференс-движка. Реальная реализация
// (CGO над llama.cpp) и stub соблюдают его одинаково, что позволяет
// собирать бинарь без CGO для тестирования прочей логики.
type Engine interface {
	// Generate выполняет один запрос. systemPrompt и userPrompt модель
	// получит как чат: system role + user role. Реализация сама форматирует
	// под токенайзер модели.
	Generate(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions) (string, error)

	// Stream — потоковая генерация. tokens закрывается, когда генерация
	// завершилась штатно или была отменена через ctx.
	Stream(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions, tokens chan<- string) error

	Close() error
}

// New создаёт движок для выбранного провайдера. Реализация local доступна
// всегда (в stub-сборке — заглушка), pollinations работает через HTTP и
// собирается без CGO.
func New(p Params) (Engine, error) {
	switch p.Provider {
	case ProviderPollinations:
		return NewPollinations(p)
	case "", ProviderLocal, ProviderAuto:
		if p.ModelPath == "" {
			return nil, ErrNoLocalModel
		}
		return newLocalEngine(p)
	default:
		return nil, fmt.Errorf("llm: unknown provider %q (expected local, pollinations or auto)", p.Provider)
	}
}
