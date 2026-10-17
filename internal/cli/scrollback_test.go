package cli

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
)

// Транскрипт не должен расти без предела: кадр строится заново на каждом
// обновлении, и лог сборки в сотни тысяч строк встал бы интерфейс.
func TestScrollbackIsTrimmed(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 80, 24
	for i := 0; i < maxScrollbackLines*2; i++ {
		m.addLine("line " + strings.Repeat("x", 20))
	}
	lines := strings.Split(m.content, "\n")
	if len(lines) > maxScrollbackLines+1 {
		t.Fatalf("transcript has %d lines, want at most %d", len(lines), maxScrollbackLines)
	}
	if !strings.Contains(lines[0], "lines trimmed") {
		t.Fatalf("the trim must be visible, not silent: %q", lines[0])
	}
	// Последние строки на месте: обрезается начало, а не хвост.
	if !strings.Contains(m.content, "x"+strings.Repeat("x", 19)) {
		t.Fatalf("the newest lines must survive the trim:\n%s", m.content[len(m.content)-200:])
	}
}

// Обрезка не должна уводить экран: кадр считается от конца транскрипта,
// поэтому удалённые сверху строки уезжают вместе с ним.
func TestTrimKeepsTheView(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 80, 10
	for i := 0; i < maxScrollbackLines+50; i++ {
		m.addLine("row")
	}
	m.scrollOffset = 0
	before := strings.Join(strings.Split(m.render(), "\n"), "|")
	m.addLine("last row of the world")
	after := strings.Join(strings.Split(m.render(), "\n"), "|")
	if !strings.Contains(after, "last row of the world") {
		t.Fatalf("the new line must be visible:\n%s", after)
	}
	if strings.Count(before, "row") > strings.Count(after, "row") {
		t.Fatalf("trimming moved the view:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Живой вывод не должен выбрасывать пользователя вниз, если тот ушёл читать
// выше: иначе «сколько строк внизу» врёт и PgDn прыгает через пол-лога.
func TestLiveOutputKeepsScrollAnchor(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 80, 12
	m.content = strings.Repeat("old line\n", 100)
	m.scrollOffset = 2
	// Фоновая команда: вывод принимается, только пока она идёт.
	m.running, m.state = true, tuiRunning
	m.cmdCh = make(chan tea.Msg, 1)

	mm, _ := m.appendCmdOutput(cmdOutMsg{text: "one\ntwo\nthree\n"})
	got := mm.(tuiModel)
	if got.scrollOffset != 5 {
		t.Fatalf("scrollOffset = %d, want 5: three new lines below the view", got.scrollOffset)
	}
	if !strings.Contains(got.runningLine(), "↓5 below") {
		t.Fatalf("the running line must show how far the user is from the end: %q", got.runningLine())
	}
}

// Подсказка «↓N below» кликабельна и возвращает в конец лога.
func TestClickBelowHintJumpsToEnd(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 80, 12
	m.running = true
	m.state = tuiRunning
	m.cmdCommand = "go build ./..."
	m.scrollOffset = 4

	r := clickRegionFor(t, m, clickJumpToEnd, 0)
	if row := frameRow(t, m, r.y); !strings.Contains(row, "↓4 below") {
		t.Fatalf("the clickable row must show the hint:\n%s", row)
	}
	got := click(m, r.x0, r.y)
	if got.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d after the click, want 0", got.scrollOffset)
	}
	// Внизу подсказки быть не должно: прыгать некуда.
	if strings.Contains(got.runningLine(), "below") {
		t.Errorf("the hint must disappear at the end of the log: %q", got.runningLine())
	}
	if regions := got.jumpToEndRegions(); len(regions) != 0 {
		t.Errorf("no clickable hint expected at the end, got %+v", regions)
	}
}

// /clear должен обнулить и счётчик строк, иначе следующая обрезка выбросит
// половину свежего экрана.
func TestClearResetsScrollbackCounter(t *testing.T) {
	m := newTestTui()
	for i := 0; i < maxScrollbackLines+10; i++ {
		m.addLine("row")
	}
	m.clearContent()
	if m.scrollbackLines != 0 {
		t.Fatalf("scrollbackLines = %d after /clear, want 0", m.scrollbackLines)
	}
	for i := 0; i < 10; i++ {
		m.addLine("fresh")
	}
	if !strings.HasSuffix(strings.TrimSpace(m.content), "fresh") {
		t.Fatalf("fresh lines must stay:\n%s", m.content)
	}
	if strings.Contains(m.content, "lines trimmed") {
		t.Fatalf("a short transcript must not be trimmed:\n%s", m.content)
	}
}

// Пометка об обрезке должна вести к полному логу, иначе обрезанный вывод
// выглядит как молчание команды.
func TestTrimNotePointsAtAuditLog(t *testing.T) {
	m := newTestTui()
	m.s.cfg.AuditFile = "/tmp/dmsh-audit.log"
	for i := 0; i < maxScrollbackLines+5; i++ {
		m.addLine("row")
	}
	if !strings.Contains(m.content, "full log: /tmp/dmsh-audit.log") {
		t.Fatalf("the trim note must point at the audit log:\n%.200s", m.content)
	}
}
