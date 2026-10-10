package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, raw string) Result {
	t.Helper()
	var res Result
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("result must be JSON: %v (%s)", err, raw)
	}
	return res
}

func TestTodoSpec(t *testing.T) {
	spec := TodoSpec()
	if spec.Name != NameTodo || spec.Description == "" {
		t.Fatalf("spec = %+v", spec)
	}
	props, _ := spec.Parameters["properties"].(map[string]any)
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("action property missing: %v", spec.Parameters)
	}
	allowed, _ := action["enum"].([]string)
	if len(allowed) != 4 {
		t.Fatalf("enum = %v, want 4 actions", action["enum"])
	}
	required, _ := spec.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v", spec.Parameters["required"])
	}
}

func TestTodoStoreAdd(t *testing.T) {
	store := NewTodoStore()
	if got := store.Render(); got != "(todo list is empty)" {
		t.Fatalf("empty render = %q", got)
	}
	first, err := store.Add("read the log")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := store.Add("write the fix")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("ids = %d, %d, want 1, 2", first.ID, second.ID)
	}
	if store.Open() != 2 {
		t.Fatalf("open = %d, want 2", store.Open())
	}
	if !strings.Contains(store.Render(), "read the log") {
		t.Fatalf("render = %q", store.Render())
	}
}

func TestTodoStoreRejectsEmptyAndFull(t *testing.T) {
	store := NewTodoStore()
	if _, err := store.Add("   "); err == nil {
		t.Fatal("blank task must be rejected")
	}
	for i := 0; i < MaxTodoItems; i++ {
		if _, err := store.Add("task"); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if _, err := store.Add("one too many"); err == nil {
		t.Fatal("overfull list must be rejected")
	}
}

func TestTodoStoreDoneAndClear(t *testing.T) {
	store := NewTodoStore()
	item, _ := store.Add("task")
	got, ok := store.Done(item.ID)
	if !ok || !got.Done {
		t.Fatalf("Done = %+v, ok=%v", got, ok)
	}
	if store.Open() != 0 {
		t.Fatalf("open = %d, want 0", store.Open())
	}
	if _, ok := store.Done(999); ok {
		t.Fatal("unknown id must not be marked done")
	}
	if n := store.Clear(); n != 1 {
		t.Fatalf("Clear = %d, want 1", n)
	}
	if len(store.Items()) != 0 {
		t.Fatalf("items after clear = %v", store.Items())
	}
}

func TestTodoStoreItemsIsACopy(t *testing.T) {
	store := NewTodoStore()
	store.Add("task")
	items := store.Items()
	items[0].Task = "mutated"
	if store.Items()[0].Task != "task" {
		t.Fatal("Items must return a copy")
	}
}

// Задача хранится одной строкой: переносы внутри неё давали пустые строки
// в /todo и ломали колонку статусов.
func TestTodoStoreAddNormalizesToOneLine(t *testing.T) {
	store := NewTodoStore()
	item, err := store.Add("  fix\n\n the\n  log  ")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if item.Task != "fix the log" {
		t.Fatalf("task = %q, want %q", item.Task, "fix the log")
	}
	if rows := strings.Split(store.Render(), "\n"); len(rows) != 1 {
		t.Fatalf("one task must render as one row: %q", store.Render())
	}
	if !strings.Contains(store.Render(), "open [1] fix the log") {
		t.Fatalf("render = %q", store.Render())
	}
}

func TestTodoRunnerAddListDoneClear(t *testing.T) {
	store := NewTodoStore()
	run := NewTodoRunner(store)
	ctx := context.Background()

	call := func(args string) Result {
		t.Helper()
		out, err := run(ctx, args)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		return decode(t, out)
	}

	res := call(`{"action":"add","task":"step one"}`)
	if !res.OK || !strings.Contains(res.Content, "step one") {
		t.Fatalf("add = %+v", res)
	}
	res = call(`{"action":"list"}`)
	if !res.OK || !strings.Contains(res.Content, "step one") {
		t.Fatalf("list = %+v", res)
	}
	res = call(`{"action":"done","id":1}`)
	if !res.OK || !strings.Contains(res.Content, "open tasks left: 0") {
		t.Fatalf("done = %+v", res)
	}
	res = call(`{"action":"clear"}`)
	if !res.OK || !strings.Contains(res.Content, "cleared 1") {
		t.Fatalf("clear = %+v", res)
	}
	if res := call(`{"action":"list"}`); !strings.Contains(res.Content, "empty") {
		t.Fatalf("list after clear = %+v", res)
	}
}

func TestTodoRunnerErrors(t *testing.T) {
	run := NewTodoRunner(NewTodoStore())
	ctx := context.Background()

	cases := []struct {
		args string
		want string
	}{
		{`{"action":"done"}`, "id is required"},
		{`{"action":"done","id":5}`, "no task with id 5"},
		{`{"action":"add"}`, "task is required"},
		{`{"action":"explode"}`, "unknown action"},
		{`not-json`, "invalid arguments"},
	}
	for _, c := range cases {
		out, err := run(ctx, c.args)
		if err == nil {
			t.Fatalf("%s: expected error, got %q", c.args, out)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not mention %q", c.args, err, c.want)
		}
	}
}

func TestAskSpecRequiresQuestion(t *testing.T) {
	spec := AskSpec()
	if spec.Name != NameAsk {
		t.Fatalf("name = %q", spec.Name)
	}
	required, _ := spec.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "question" {
		t.Fatalf("required = %v", spec.Parameters["required"])
	}
	if _, err := json.Marshal(spec); err != nil {
		t.Fatalf("spec must be serializable: %v", err)
	}
}

func TestWebSearchSpecMentionsModel(t *testing.T) {
	spec := WebSearchSpec("gemini-search")
	if spec.Name != NameWebSearch {
		t.Fatalf("name = %q", spec.Name)
	}
	if !strings.Contains(spec.Description, "gemini-search") {
		t.Fatalf("description should mention the search model: %q", spec.Description)
	}
	required, _ := spec.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("required = %v", spec.Parameters["required"])
	}
	if _, err := json.Marshal(spec); err != nil {
		t.Fatalf("spec must be serializable: %v", err)
	}
}
