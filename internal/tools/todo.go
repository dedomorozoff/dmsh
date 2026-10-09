package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/dedomorozoff/dmsh/internal/llm"
)

// Действия инструмента todo.
const (
	TodoAdd   = "add"
	TodoList  = "list"
	TodoDone  = "done"
	TodoClear = "clear"
)

// MaxTodoItems ограничивает список: длиннее он только мешает модели.
const MaxTodoItems = 50

// TodoItem — одна задача в списке модели.
type TodoItem struct {
	ID   int    `json:"id"`
	Task string `json:"task"`
	Done bool   `json:"done"`
}

// TodoStore — список задач текущей сессии. Живёт в памяти: закрыли
// dmsh — список закрыт вместе с ним.
type TodoStore struct {
	mu    sync.Mutex
	items []TodoItem
	next  int
}

// NewTodoStore возвращает пустой список задач.
func NewTodoStore() *TodoStore {
	return &TodoStore{next: 1}
}

// Add добавляет задачу и возвращает её с присвоенным id.
func (s *TodoStore) Add(task string) (TodoItem, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return TodoItem{}, fmt.Errorf("task is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) >= MaxTodoItems {
		return TodoItem{}, fmt.Errorf("todo list is full (max %d items)", MaxTodoItems)
	}
	item := TodoItem{ID: s.next, Task: task}
	s.next++
	s.items = append(s.items, item)
	return item, nil
}

// Done отмечает задачу выполненной по id.
func (s *TodoStore) Done(id int) (TodoItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Done = true
			return s.items[i], true
		}
	}
	return TodoItem{}, false
}

// Clear удаляет все задачи.
func (s *TodoStore) Clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	s.items = nil
	return n
}

// Items возвращает копию списка.
func (s *TodoStore) Items() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TodoItem, len(s.items))
	copy(out, s.items)
	return out
}

// Open возвращает количество незакрытых задач.
func (s *TodoStore) Open() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, it := range s.items {
		if !it.Done {
			n++
		}
	}
	return n
}

// Render возвращает список в компактном виде — и для модели, и для /todo.
func (s *TodoStore) Render() string {
	items := s.Items()
	if len(items) == 0 {
		return "(todo list is empty)"
	}
	var b strings.Builder
	for _, it := range items {
		state := "open"
		if it.Done {
			state = "done"
		}
		fmt.Fprintf(&b, "%s [%d] %s (%s)\n", state, it.ID, it.Task, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

// TodoArgs — аргументы инструмента todo.
type TodoArgs struct {
	Action string `json:"action"`
	Task   string `json:"task,omitempty"`
	ID     int    `json:"id,omitempty"`
}

// TodoSpec — схема инструмента todo.
func TodoSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: NameTodo,
		Description: "Keep a checklist of the steps of the current task. " +
			"Use action=add before starting a multi-step task, action=done after each completed step, " +
			"action=list to re-read the list, action=clear to drop it.",
		Parameters: objectSchema(map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{TodoAdd, TodoList, TodoDone, TodoClear},
				"description": "add | list | done | clear.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Task text. Required for action=add.",
			},
			"id": map[string]any{
				"type":        "integer",
				"description": "Task id to close. Required for action=done.",
			},
		}, "action"),
	}
}

// NewTodoRunner возвращает обработчик инструмента todo поверх store.
// Список принадлежит сессии, поэтому создаётся вместе с ней.
func NewTodoRunner(store *TodoStore) Runner {
	return func(_ context.Context, rawArgs string) (string, error) {
		var args TodoArgs
		if err := json.Unmarshal([]byte(orEmptyObject(rawArgs)), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(args.Action)) {
		case TodoAdd:
			item, err := store.Add(args.Task)
			if err != nil {
				return "", err
			}
			return EncodeResult(Result{OK: true, Content: fmt.Sprintf("added [%d] %s\n\n%s", item.ID, item.Task, store.Render())}), nil
		case TodoList:
			return EncodeResult(Result{OK: true, Content: store.Render()}), nil
		case TodoDone:
			if args.ID <= 0 {
				return "", fmt.Errorf("id is required for action=%s", TodoDone)
			}
			item, ok := store.Done(args.ID)
			if !ok {
				return "", fmt.Errorf("no task with id %d", args.ID)
			}
			return EncodeResult(Result{OK: true, Content: fmt.Sprintf("closed [%d] %s\nopen tasks left: %d\n\n%s",
				item.ID, item.Task, store.Open(), store.Render())}), nil
		case TodoClear:
			n := store.Clear()
			return EncodeResult(Result{OK: true, Content: "cleared " + strconv.Itoa(n) + " task(s)"}), nil
		default:
			return "", fmt.Errorf("unknown action %q (expected add, list, done or clear)", args.Action)
		}
	}
}
