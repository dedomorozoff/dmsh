package cli

import (
	"fmt"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// Список задач модели — окно, а не текст в транскрипте: это единственное
// место, где пользователь и модель смотрят на одно и то же состояние, и
// должен видеть его без прокрутки. Плюс состояние задачи можно поправить
// самому: модель считает «сделано» по своей картине мира, а человек знает
// лучше.

// todoMaxVisible — сколько задач помещается в окно.
const todoMaxVisible = 10

const todoTitle = "todo"

// todoItems — задачи для показа. Список живёт в сессии и может меняться из
// tools, поэтому каждый раз берётся свежим.
func (m tuiModel) todoItems() []tools.TodoItem {
	if m.s == nil || m.s.todos == nil {
		return nil
	}
	return m.s.todos.Items()
}

// openTodoMenu открывает окно задач. Пустой список — тоже окно: пользователь
// должен увидеть, что список есть и он пуст, а не гадать, где он.
func (m tuiModel) openTodoMenu() tuiModel {
	m.state = tuiTodoMenu
	m.todoIdx = 0
	return m
}

// todoOpenCount — сколько задач ещё открыто. Считается по свежему списку:
// он меняется и из tools, и из этого окна.
func todoOpenCount(m tuiModel) int {
	n := 0
	for _, it := range m.todoItems() {
		if !it.Done {
			n++
		}
	}
	return n
}

// todoLayout — строки окна и раскладка: первая задача, её номер в списке и
// полуинтервал показанных. Курсор и клик считаются по этим же числам.
type todoLayout struct {
	rows        []string
	items       []tools.TodoItem
	start, end  int
	firstRowIdx int
}

func (m tuiModel) todoLayout() todoLayout {
	items := m.todoItems()
	start, end := scrollWindow(m.todoIdx, len(items), todoMaxVisible)

	rows := make([]string, 0, end-start+2)
	rows = append(rows, fitWidth(m.todoHeader(items), m.width))
	first := len(rows)
	if len(items) == 0 {
		rows = append(rows, fitWidth(colorGray+"the list is empty — the model adds tasks while it works"+colorReset, m.width))
	}
	for i := start; i < end; i++ {
		// Состояние и выделение — разные вещи: «✓» остаётся даже когда
		// задача выбрана, иначе сделанная выглядит открытой.
		marker, style := "  ", colorReset
		if items[i].Done {
			marker, style = "✓ ", colorGray
		}
		if i == m.todoIdx {
			marker, style = "> ", colorYellow+colorBold
		}
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s[%d]%s %s",
			marker, colorCyan, items[i].ID, colorReset, style+items[i].Task+colorReset), m.width))
	}
	rows = append(rows, fitWidth(fmt.Sprintf("%s↑/↓ pick · Enter or click toggles · c clears · Esc close%s",
		colorGray, colorReset), m.width))
	return todoLayout{rows: rows, items: items, start: start, end: end, firstRowIdx: first}
}

func (m tuiModel) todoHeader(items []tools.TodoItem) string {
	if len(items) == 0 {
		return colorGray + "no tasks yet" + colorReset
	}
	open := todoOpenCount(m)
	return fmt.Sprintf("%s%d tasks · %d open%s", colorCyan, len(items), open, colorReset)
}

// todoModal описывает окно задач для рендера.
func (m tuiModel) todoModal() modalBox {
	l := m.todoLayout()
	cursorY := len(l.rows) - 1
	if l.end > l.start {
		cursorY = l.firstRowIdx + m.todoIdx - l.start
	}
	return modalBox{title: todoTitle, rows: l.rows, cursorX: 2, cursorY: max(0, cursorY)}
}

// handleTodoMenuKey обрабатывает клавиши в окне задач. Пробел работает так же,
// как Enter: окно открывают мышью, а значит и закрывать будут мышью.
func (m tuiModel) handleTodoMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.todoItems()
	switch {
	case msg.Code == tea.KeyEscape || msg.Keystroke() == "ctrl+c":
		return m.closeTodoMenu(), nil
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.todoMove(-1, len(items))
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.todoMove(1, len(items))
		return m, nil
	case msg.Code == tea.KeyEnter || msg.Code == tea.KeySpace:
		m.toggleTodoAt(m.todoIdx, items)
		return m, nil
	case msg.Keystroke() == "c":
		if m.s != nil && m.s.todos != nil {
			n := m.s.todos.Clear()
			if n > 0 {
				m.addLine(fmt.Sprintf("(%scleared %d tasks%s)", colorGray, n, colorReset))
			}
		}
		m.todoIdx = 0
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) todoMove(dir, n int) {
	if n <= 0 {
		m.todoIdx = 0
		return
	}
	m.todoIdx = ((m.todoIdx+dir)%n + n) % n
}

// toggleTodoAt переключает задачу: сделанную можно вернуть в работу. Список
// мог измениться, пока окно было открыто, поэтому сверяемся по номеру строки,
// а не по сохранённому индексу.
func (m tuiModel) toggleTodoAt(idx int, items []tools.TodoItem) tuiModel {
	if m.s == nil || m.s.todos == nil || idx < 0 || idx >= len(items) {
		return m
	}
	item := items[idx]
	updated, ok := m.s.todos.Set(item.ID, !item.Done)
	if !ok {
		return m
	}
	word := "reopened"
	if updated.Done {
		word = "done"
	}
	m.addLine(fmt.Sprintf("%stodo %s: %s%s", colorGray, word, updated.Task, colorReset))
	return m
}

// closeTodoMenu возвращает обычный экран.
func (m tuiModel) closeTodoMenu() tuiModel {
	m.state = tuiIdle
	m.todoIdx = 0
	return m
}
