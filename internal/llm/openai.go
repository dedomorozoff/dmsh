package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Значения по умолчанию для удалённых провайдеров.
const (
	// DefaultPollinationsBaseURL — OpenAI-совместимый endpoint Pollinations.
	DefaultPollinationsBaseURL = "https://gen.pollinations.ai/v1"
	DefaultPollinationsModel   = "openai"
	// DefaultRemoteTimeout — потолок одного запроса к удалённому API.
	DefaultRemoteTimeout = 180 * time.Second
	// maxRemoteErrorBody ограничивает тело ответа при ошибке, чтобы
	// сообщение об ошибке оставалось читаемым.
	maxRemoteErrorBody = 4 << 10
	// maxStreamLine ограничивает длину одной SSE-строки: аргументы
	// tool_calls могут приходить большими фрагментами.
	maxStreamLine = 4 << 20
)

// openaiEngine работает через любой OpenAI-совместимый chat/completions:
// и Pollinations, и Ollama говорят на этом диалекте. Различаются они
// только адресом, моделью по умолчанию и подсказками в ошибках.
type openaiEngine struct {
	baseURL string
	model   string
	apiKey  string
	// name попадает в текст ошибок: «ollama: не удалось подключиться» и
	// «pollinations: HTTP 402» читаются по-разному.
	name   string
	client *http.Client
}

// NewPollinations создаёт удалённый движок. Соединение не проверяется до
// первого запроса: сеть бывает включена позже, а провайдер должен ещё иметь
// ключ (см. Params.APIKey).
func NewPollinations(p Params) (Engine, error) {
	return newOpenAIEngine(remoteProviderPollinations, DefaultPollinationsBaseURL, DefaultPollinationsModel, p)
}

func newOpenAIEngine(name, defaultBase, defaultModel string, p Params) (Engine, error) {
	base := strings.TrimSpace(p.RemoteBaseURL)
	if base == "" {
		base = defaultBase
	}
	model := strings.TrimSpace(p.RemoteModel)
	if model == "" {
		model = defaultModel
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultRemoteTimeout
	}
	client, err := p.Proxy.Client(timeout)
	if err != nil {
		return nil, fmt.Errorf("llm: proxy: %w", err)
	}
	return &openaiEngine{
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		apiKey:  strings.TrimSpace(p.APIKey),
		name:    name,
		client:  client,
	}, nil
}

// chatURL — адрес самого запроса. Провайдеры задают базу так же, как это
// делает любой OpenAI SDK (https://gen.pollinations.ai/v1), а маршрут
// добавляется здесь. Уже готовый адрес не дополняется: старые настройки с
// полным endpoint'ом должны продолжать работать.
func chatURL(base string) string {
	return routeURL(base, "/chat/completions")
}

// routeURL приписывает маршрут к базе. База, которая уже указывает на
// /chat/completions, приводится к общему виду, иначе маршрут удвоился бы.
func routeURL(base, route string) string {
	b := strings.TrimRight(base, "/")
	b = strings.TrimSuffix(b, "/chat/completions")
	return b + route
}

// chatBody — тело запроса к OpenAI-совместимому API.
type chatBody struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Temperature float32    `json:"temperature"`
	TopP        float32    `json:"top_p,omitempty"`
	MaxTokens   int        `json:"max_tokens,omitempty"`
	Stream      bool       `json:"stream,omitempty"`
	Tools       []wireTool `json:"tools,omitempty"`
	ToolChoice  string     `json:"tool_choice,omitempty"`
}

// wireTool — вложенное представление инструмента в OpenAI-совместимом API.
type wireTool struct {
	Type     string       `json:"type"`
	Function wireFuncSpec `json:"function"`
}

type wireFuncSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// streamChunk — один SSE-чанк. Поток отдаёт дельты контента и куски
// tool_calls; часть провайдеров вместо дельт присылает финальное сообщение
// целиком в message, поэтому поддерживаются оба варианта.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (e *openaiEngine) do(ctx context.Context, body chatBody) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL(e.baseURL), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("llm: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: %s request to %s failed: %w%s", e.name, e.baseURL, err, transportHint(e.name))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxRemoteErrorBody))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if hint := remoteErrorHint(e.name, resp.StatusCode); hint != "" {
			return nil, fmt.Errorf("llm: %s HTTP %d: %w (%s)", e.name, resp.StatusCode, errRemote, hint)
		}
		return nil, fmt.Errorf("llm: %s HTTP %d: %s", e.name, resp.StatusCode, msg)
	}
	return resp, nil
}

// errRemote помечает сетевую ошибку провайдера, чтобы вызывающая сторона
// могла отличить её от ошибок разбора ответа.
var errRemote = errors.New("remote provider request failed")

// transportHint объясняет, что делать, когда до сервера не дошли. У Ollama
// это почти всегда «сервер не запущен», и без подсказки ошибка выглядит как
// непонятный отказ соединения.
func transportHint(name string) string {
	if name != remoteProviderOllama {
		return ""
	}
	return "\n  hint: is ollama running? start it with `ollama serve` (default http://127.0.0.1:11434)"
}

// remoteErrorHint переводит частые ответы провайдера в actionable текст.
// У Pollinations ключ обязателен для генерации, у Ollama — модель должна быть
// скачана заранее.
func remoteErrorHint(name string, code int) string {
	if name == remoteProviderOllama {
		if code == http.StatusNotFound {
			return "model not found — pull it first: ollama pull <model>"
		}
		return ""
	}
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "the provider needs an API key — set POLLINATIONS_API_KEY (get one at https://enter.pollinations.ai/keys)"
	case http.StatusPaymentRequired:
		return "out of Pollen — top up at https://enter.pollinations.ai/pollen or pick another model with --remote-model"
	case http.StatusTooManyRequests:
		return "rate limited — wait a moment, or lower --max-tokens"
	case http.StatusNotFound:
		return "unknown model or endpoint — see the catalog at https://gen.pollinations.ai/models"
	default:
		return ""
	}
}

func (e *openaiEngine) singleTurnBody(systemPrompt, userPrompt string, opts SamplingOptions) chatBody {
	var messages []Message
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, Message{Role: RoleSystem, Content: systemPrompt})
	}
	if strings.TrimSpace(userPrompt) != "" {
		messages = append(messages, Message{Role: RoleUser, Content: userPrompt})
	}
	return chatBody{
		Model:       e.model,
		Messages:    messages,
		Temperature: opts.Temperature,
		TopP:        opts.TopP,
		MaxTokens:   opts.MaxTokens,
	}
}

// Generate выполняет один запрос без стриминга.
func (e *openaiEngine) Generate(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions) (string, error) {
	resp, err := e.do(ctx, e.singleTurnBody(systemPrompt, userPrompt, opts))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	msg, err := decodeChatResponse(resp.Body)
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}

// Stream выполняет запрос и отдаёт текст ответа по кускам.
func (e *openaiEngine) Stream(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions, out chan<- string) error {
	defer close(out)

	body := e.singleTurnBody(systemPrompt, userPrompt, opts)
	body.Stream = true

	resp, err := e.do(ctx, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	acc := &streamAccumulator{}
	rd := newSSEReader(resp.Body)
	for {
		chunk, err := rd.next()
		if err != nil {
			if err == errStreamDone {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		text := chunk.apply(acc)
		if text == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- text:
		}
	}
}

// Chat выполняет один заход диалога с необязательными инструментами.
// Если в запросе задан Deltas, ответ приходит потоком по кускам текста,
// иначе одним сообщением.
func (e *openaiEngine) Chat(ctx context.Context, req ChatRequest) (Message, error) {
	if len(req.Messages) == 0 {
		return Message{}, fmt.Errorf("llm: empty chat request")
	}

	body := e.singleTurnBody("", "", req.Options)
	body.Messages = req.Messages
	if len(req.Tools) > 0 {
		body.Tools = normalizeTools(req.Tools)
		body.ToolChoice = toolChoice(req.ToolChoice)
	}

	if req.Deltas == nil {
		resp, err := e.do(ctx, body)
		if err != nil {
			return Message{}, err
		}
		defer func() { _ = resp.Body.Close() }()
		return decodeChatResponse(resp.Body)
	}

	body.Stream = true
	resp, err := e.do(ctx, body)
	if err != nil {
		close(req.Deltas)
		return Message{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	defer close(req.Deltas)

	acc := &streamAccumulator{}
	rd := newSSEReader(resp.Body)
	for {
		chunk, err := rd.next()
		if err != nil {
			if err == errStreamDone {
				return acc.message(), nil
			}
			if ctx.Err() != nil {
				return Message{}, ctx.Err()
			}
			return Message{}, err
		}
		text := chunk.apply(acc)
		if text == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return Message{}, ctx.Err()
		case req.Deltas <- text:
		}
	}
}

func (e *openaiEngine) Close() error { return nil }

// decodeChatResponse разбирает тело ответа chat/completions. Провайдер в
// тексте ошибки не нужен: он уже назван в do().
func decodeChatResponse(body io.Reader) (Message, error) {
	var parsed chatResponse
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return Message{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Message{}, fmt.Errorf("llm: provider error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, errors.New("llm: provider returned no choices")
	}
	msg := parsed.Choices[0].Message
	if msg.Role == "" {
		msg.Role = RoleAssistant
	}
	return msg, nil
}

// toolChoice нормализует выбор инструментов: пустое значение означает
// "auto" — модель сама решает, вызывать инструмент или отвечать текстом.
func toolChoice(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "auto" {
		return "auto"
	}
	return v
}

// normalizeTools приводит плоское описание инструмента к вложенному
// формату {"type":"function","function":{...}} и проставляет type.
func normalizeTools(tools []ToolSpec) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		typ := t.Type
		if typ == "" {
			typ = "function"
		}
		out = append(out, wireTool{
			Type: typ,
			Function: wireFuncSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// streamAccumulator склеивает SSE-дельты в полное сообщение ассистента.
type streamAccumulator struct {
	content strings.Builder
	// calls хранит частичные вызовы инструментов по индексу, чтобы
	// дозаписываемые аргументы не терялись между чанками.
	calls map[int]*ToolCall
	order []int
}

func (a *streamAccumulator) call(idx int) *ToolCall {
	if a.calls == nil {
		a.calls = make(map[int]*ToolCall)
	}
	if c, ok := a.calls[idx]; ok {
		return c
	}
	c := &ToolCall{Index: idx, Type: "function"}
	a.calls[idx] = c
	a.order = append(a.order, idx)
	return c
}

func (a *streamAccumulator) message() Message {
	msg := Message{Role: RoleAssistant, Content: a.content.String()}
	for _, idx := range a.order {
		if c := a.calls[idx]; c != nil {
			msg.ToolCalls = append(msg.ToolCalls, *c)
		}
	}
	return msg
}

// apply накладывает чанк на накопленный результат и возвращает текст,
// который нужно отдать потребителю (пустая строка — ничего не отдаём).
func (c *streamChunk) apply(acc *streamAccumulator) string {
	if c.Error != nil && c.Error.Message != "" {
		return ""
	}
	if len(c.Choices) == 0 {
		return ""
	}
	ch := c.Choices[0]
	if ch.Delta.Content != "" {
		acc.content.WriteString(ch.Delta.Content)
		return ch.Delta.Content
	}
	for i, tc := range ch.Delta.ToolCalls {
		idx := tc.Index
		if idx == 0 && i > 0 {
			idx = i
		}
		acc.mergeTool(idx, tc, false)
	}
	// Провайдеры без стрима присылают одно сообщение целиком.
	if ch.Message.Content != "" && acc.content.Len() == 0 {
		acc.content.WriteString(ch.Message.Content)
		for i, tc := range ch.Message.ToolCalls {
			acc.mergeTool(i, tc, true)
		}
		return ch.Message.Content
	}
	return ""
}

func (a *streamAccumulator) mergeTool(idx int, tc ToolCall, replace bool) {
	target := a.call(idx)
	if replace {
		target.ID, target.Type, target.Name, target.Arguments = tc.ID, tc.Type, tc.Name, tc.Arguments
		return
	}
	if tc.ID != "" {
		target.ID = tc.ID
	}
	if tc.Type != "" {
		target.Type = tc.Type
	}
	if tc.Name != "" {
		target.Name = tc.Name
	}
	if tc.Arguments != "" {
		target.Arguments += tc.Arguments
	}
}

var errStreamDone = fmt.Errorf("llm: stream finished")

// sseReader разбирает поток `text/event-stream`. Экземпляр создаётся один
// раз на ответ: буфер сканера переживает между вызовами next.
type sseReader struct {
	sc *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 8*1024), maxStreamLine)
	return &sseReader{sc: sc}
}

func (s *sseReader) next() (*streamChunk, error) {
	for s.sc.Scan() {
		line := strings.TrimSpace(s.sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil, errStreamDone
		}
		if data == "" {
			continue
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		return &chunk, nil
	}
	if err := s.sc.Err(); err != nil {
		return nil, fmt.Errorf("llm: read stream: %w", err)
	}
	return nil, errStreamDone
}
