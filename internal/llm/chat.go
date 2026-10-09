package llm

import (
	"context"
	"encoding/json"
	"strings"
)

// Role — автор сообщения в диалоге с моделью.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall — запрос модели на вызов инструмента. Arguments хранит сырой
// JSON-объект аргументов: разбирать его должен вызывающий код, потому что
// только он знает типы инструментов.
//
// В OpenAI-совместимом API имя и аргументы лежат во вложенном объекте
// function, поэтому сериализация настроена на него; плоский вид
// ({ "name": ... }) тоже принимается — некоторые шлюзы отдают его при
// разборе ответа.
type ToolCall struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
	// Index приходит в потоковых чанках, чтобы собрать аргументы,
	// разбитые между несколькими дельтами.
	Index     int    `json:"index,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Index    int    `json:"index,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
	// Плоские поля — запасной вариант для нестандартных ответов.
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// MarshalJSON пишет вызов вложенным объектом function.
func (c ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireToolCall{
		ID:    c.ID,
		Type:  c.Type,
		Index: c.Index,
		Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: c.Name, Arguments: c.Arguments},
	})
}

// UnmarshalJSON принимает и вложенный, и плоский формат вызова.
func (c *ToolCall) UnmarshalJSON(data []byte) error {
	var w wireToolCall
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	c.ID = w.ID
	c.Type = w.Type
	c.Index = w.Index
	c.Name = w.Function.Name
	c.Arguments = w.Function.Arguments
	if c.Name == "" {
		c.Name = w.Name
	}
	if c.Arguments == "" {
		c.Arguments = w.Arguments
	}
	return nil
}

// DecodeArgs разбирает аргументы вызова в v. Пустые аргументы допустимы:
// модель может отправить вызов без параметров.
func (c ToolCall) DecodeArgs(v any) error {
	raw := strings.TrimSpace(c.Arguments)
	if raw == "" {
		raw = "{}"
	}
	return json.Unmarshal([]byte(raw), v)
}

// Message — одно сообщение диалога. Поддерживается ровно тот подмножество
// OpenAI-совместимого формата, которое нужно dmsh: текст плюс вызовы
// инструментов и результат одного вызова.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolSpec — описание одного инструмента в формате OpenAI function calling.
type ToolSpec struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatRequest — один заход диалога с необязательным function calling.
// Deltas, если задан, получает потоковые фрагменты текста; реализация
// обязана закрыть канал перед возвратом, в том числе при ошибке.
type ChatRequest struct {
	Messages   []Message
	Tools      []ToolSpec
	ToolChoice string
	Options    SamplingOptions
	Deltas     chan<- string
}

// ToolEngine — движок, умеющий function calling. Локальный llama.cpp без
// специальной постобработки этого не поддерживает, поэтому наличие
// инструментов проверяется через SupportsTools, а не через Engine.
type ToolEngine interface {
	Chat(ctx context.Context, req ChatRequest) (Message, error)
}

// SupportsTools сообщает, умеет ли движок вызов инструментов.
func SupportsTools(e Engine) bool {
	_, ok := e.(ToolEngine)
	return ok
}

// NewToolResult собирает сообщение с результатом вызова инструмента.
func NewToolResult(call ToolCall, content string) Message {
	return Message{
		Role:       RoleTool,
		Name:       call.Name,
		ToolCallID: call.ID,
		Content:    content,
	}
}
