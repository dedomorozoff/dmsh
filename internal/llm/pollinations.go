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

// Значения по умолчанию для удалённого провайдера.
const (
	DefaultPollinationsBaseURL = "https://text.pollinations.ai/openai"
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

// pollinationsEngine работает через OpenAI-совместимый endpoint Pollinations.
// Авторизация не используется: сервис доступен анонимно.
type pollinationsEngine struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewPollinations создаёт удалённый движок. Ошибок до первого запроса не
// возвращает: сетевой доступ проверяется при Generate/Stream/Chat.
func NewPollinations(p Params) (Engine, error) {
	base := strings.TrimSpace(p.RemoteBaseURL)
	if base == "" {
		base = DefaultPollinationsBaseURL
	}
	model := strings.TrimSpace(p.RemoteModel)
	if model == "" {
		model = DefaultPollinationsModel
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultRemoteTimeout
	}
	return &pollinationsEngine{
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}, nil
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

func (e *pollinationsEngine) do(ctx context.Context, body chatBody) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("llm: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: pollinations request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxRemoteErrorBody))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if hint := remoteErrorHint(resp.StatusCode); hint != "" {
			return nil, fmt.Errorf("llm: pollinations HTTP %d: %w (%s)", resp.StatusCode, errRemote, hint)
		}
		return nil, fmt.Errorf("llm: pollinations HTTP %d: %s", resp.StatusCode, msg)
	}
	return resp, nil
}

// errRemote помечает сетевую ошибку провайдера, чтобы вызывающая сторона
// могла отличить её от ошибок разбора ответа.
var errRemote = errors.New("remote provider request failed")

// remoteErrorHint переводит частые ответы Pollinations в actionable текст:
// анонимный доступ допускает один запрос за раз, поэтому чаще всего
// 429 означает «слишком быстро», а не «сломанный запрос».
func remoteErrorHint(code int) string {
	switch code {
	case http.StatusTooManyRequests:
		return "anonymous access allows one request at a time — wait ~15s and retry, or use --remote-base-url with your own endpoint"
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		return "the model requires a token; dmsh does not send tokens — use --remote-base-url with your own endpoint or pick a free model with --remote-model"
	default:
		return ""
	}
}

func (e *pollinationsEngine) singleTurnBody(systemPrompt, userPrompt string, opts SamplingOptions) chatBody {
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
func (e *pollinationsEngine) Generate(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions) (string, error) {
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
func (e *pollinationsEngine) Stream(ctx context.Context, systemPrompt, userPrompt string, opts SamplingOptions, out chan<- string) error {
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
func (e *pollinationsEngine) Chat(ctx context.Context, req ChatRequest) (Message, error) {
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

func (e *pollinationsEngine) Close() error { return nil }

func decodeChatResponse(body io.Reader) (Message, error) {
	var parsed chatResponse
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return Message{}, fmt.Errorf("llm: decode pollinations response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Message{}, fmt.Errorf("llm: pollinations error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, fmt.Errorf("llm: pollinations returned no choices")
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
