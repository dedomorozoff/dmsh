package cli

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// ansiStripRows — строки окна без цвета: в тестах сравнивается текст, а не
// escape-последовательности.
func ansiStripRows(rows []string) string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, ansi.Strip(r))
	}
	return strings.Join(out, "\n")
}

// todoTuiWithTasks — модель с заполненным списком задач: окно задач должно
// проверяться на настоящем содержимом, а не на пустом.
func todoTuiWithTasks(n int) tuiModel {
	m := newTestTui()
	m.width, m.height = 100, 24
	m.s.todos = tools.NewTodoStore()
	for i := 0; i < n; i++ {
		if _, err := m.s.todos.Add("step " + string(rune('A'+i))); err != nil {
			panic(err)
		}
	}
	return m
}

// Клик по задаче отмечает её сделанной: список задач — единственное место, где
// пользователь может поправить модель, и делается это без команды модели.
func TestTodoClickToggles(t *testing.T) {
	m := todoTuiWithTasks(3)
	m = m.openTodoMenu()

	r := clickRegionFor(t, m, clickTodoItem, 0)
	if row := frameRow(t, m, r.y); !strings.Contains(row, "step A") {
		t.Fatalf("the clickable row is not the task:\n%s", row)
	}
	got := click(m, r.x0, r.y)
	if !got.s.todos.Items()[0].Done {
		t.Fatal("clicking a task must mark it done")
	}
	if got.state != tuiTodoMenu {
		t.Errorf("state = %v, want the window to stay open", got.state)
	}
	if !strings.Contains(got.content, "todo done: step A") {
		t.Errorf("the user must see what happened:\n%s", got.content)
	}
	// Повторный клик возвращает задачу в работу.
	got = click(got, r.x0, r.y)
	if got.s.todos.Items()[0].Done {
		t.Fatal("clicking a done task must reopen it")
	}
	if !strings.Contains(got.content, "todo reopened: step A") {
		t.Errorf("reopening must be visible too:\n%s", got.content)
	}
}

// Enter и пробел работают как клик: окно открывают мышью, значит и править
// будут мышью, а клавиатура не должна отставать.
func TestTodoKeysMatchClicks(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeySpace}} {
		m := todoTuiWithTasks(2).openTodoMenu()
		got := press(m, key)
		if !got.s.todos.Items()[0].Done {
			t.Fatalf("key %v must toggle the selected task", key)
		}
	}
}

// Очистка списка остаётся только с клавиатуры: это необратимое действие, и
// клик по «всё подряд» не должен уносить список задач.
func TestTodoClearNeedsExplicitKey(t *testing.T) {
	m := todoTuiWithTasks(2).openTodoMenu()
	got := click(m, 0, 0)
	if len(got.s.todos.Items()) != 2 {
		t.Fatal("a click must not clear the list")
	}
	got = press(got, tea.KeyPressMsg{Text: "c", Code: 'c'})
	if len(got.s.todos.Items()) != 0 {
		t.Fatalf("c must clear the list, left %d", len(got.s.todos.Items()))
	}
	if !strings.Contains(got.content, "cleared 2 tasks") {
		t.Errorf("clearing must be visible:\n%s", got.content)
	}
}

// Пустой список — тоже окно: пользователь должен видеть, что список есть.
func TestTodoWindowOnEmptyList(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 100, 24
	m = m.openTodoMenu()
	box := m.todoModal()
	if len(m.clickRegions()) != 0 {
		t.Fatalf("an empty list has nothing to click: %+v", m.clickRegions())
	}
	if !strings.Contains(ansiStripRows(box.rows), "empty") {
		t.Fatalf("the window must say the list is empty:\n%s", ansiStripRows(box.rows))
	}
}

// Заголовок считает открытые задачи: это и есть смысл списка.
func TestTodoHeaderCountsOpen(t *testing.T) {
	m := todoTuiWithTasks(3).openTodoMenu()
	m.s.todos.Done(1)
	if got := todoOpenCount(m); got != 2 {
		t.Fatalf("open = %d, want 2", got)
	}
	header := ansiStripRows([]string{m.todoHeader(m.todoItems())})
	if !strings.Contains(header, "3 tasks") || !strings.Contains(header, "2 open") {
		t.Fatalf("header = %q", header)
	}
}

// Стрелки листают список, Esc закрывает окно.
func TestTodoNavigation(t *testing.T) {
	m := todoTuiWithTasks(3).openTodoMenu()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.todoItems()[m.todoIdx].Task; got != "step B" {
		t.Fatalf("after Down the selection is %q", got)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %v, want idle", m.state)
	}
}
