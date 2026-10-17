package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// scrollTui — модель с длинным транскриптом, ради которого нужна полоса.
func scrollTui(t *testing.T, lines int) tuiModel {
	t.Helper()
	m := newTestTui()
	m.width, m.height = 40, 12
	m.content = strings.Repeat("line\n", lines)
	return m
}

// Полоса появляется только когда есть что прокручивать: лишняя колонка в
// каждой строке на пустом месте — цена без пользы.
func TestScrollbarOnlyWhenNeeded(t *testing.T) {
	short := scrollTui(t, 3)
	if _, ok := short.scrollbarGeom(short.scrollBodyRows()); ok {
		t.Error("a short transcript must not get a scrollbar")
	}
	if got := short.scrollbarRegions(); len(got) != 0 {
		t.Errorf("a short transcript must not be clickable on the right edge: %+v", got)
	}
	if strings.Contains(ansi.Strip(short.render()), "█") || strings.Contains(ansi.Strip(short.render()), "│") {
		t.Error("nothing should be drawn when there is nothing to scroll")
	}

	long := scrollTui(t, 200)
	if _, ok := long.scrollbarGeom(long.scrollBodyRows()); !ok {
		t.Error("a long transcript must have a scrollbar")
	}
}

// Полоса не должна ломать раскладку: ширина каждой строки остаётся прежней.
func TestScrollbarKeepsFrameWidth(t *testing.T) {
	m := scrollTui(t, 200)
	m.scrollOffset = 40
	rows := strings.Split(m.render(), "\n")
	if len(rows) != m.height {
		t.Fatalf("frame has %d rows, want %d", len(rows), m.height)
	}
	for i, row := range rows {
		if got := displayWidth(row); got > m.width {
			t.Fatalf("row %d is %d wide, want <= %d:\n%s", i, got, m.width, row)
		}
	}
}

// Ползунок должен отражать положение экрана: внизу (свежий вывод) он внизу
// трека, при прокрутке вверх — поднимается.
func TestScrollbarThumbFollowsPosition(t *testing.T) {
	m := scrollTui(t, 400)
	avail := m.scrollBodyRows()

	m.scrollOffset = 0
	bottom, ok := m.scrollbarGeom(avail)
	if !ok {
		t.Fatal("the scrollbar must exist")
	}
	if bottom.thumbBot != bottom.bottom {
		t.Fatalf("at the bottom the thumb must touch the track end: %+v", bottom)
	}

	m.scrollOffset = 200
	middle, _ := m.scrollbarGeom(avail)
	if middle.thumbBot >= bottom.thumbBot {
		t.Fatalf("scrolling into history must move the thumb up: %d vs %d", middle.thumbBot, bottom.thumbBot)
	}
	if middle.thumbTop < middle.top || middle.thumbBot > middle.bottom {
		t.Fatalf("the thumb left the track: %+v", middle)
	}
}

// Клик по дорожке листает страницу: низ — свежий вывод, верх — начало лога.
func TestClickScrollTrackScrolls(t *testing.T) {
	m := scrollTui(t, 400)
	regions := m.scrollbarRegions()
	if len(regions) == 0 {
		t.Fatal("the track must be clickable")
	}
	// Клик в верхней части трека уводит в начало лога.
	top := regions[0]
	got := click(m, top.x0, top.y)
	if got.scrollOffset <= 0 {
		t.Fatalf("a click at the top of the track must scroll up, got offset %d", got.scrollOffset)
	}
	// Клик внизу возвращает к свежему выводу.
	bottom := regions[len(regions)-1]
	got = click(got, bottom.x0, bottom.y)
	if got.scrollOffset != 0 {
		t.Fatalf("a click at the bottom of the track must return to the newest output, got %d", got.scrollOffset)
	}
}

// Дорожка — это одна колонка у правого края, а не половина экрана.
func TestScrollTrackIsOneColumn(t *testing.T) {
	m := scrollTui(t, 400)
	for _, r := range m.scrollbarRegions() {
		if r.x1-r.x0 != 1 {
			t.Fatalf("track region must be one column wide: %+v", r)
		}
		if r.x0 != m.width-1 {
			t.Fatalf("the track must sit at the right edge: %+v", r)
		}
	}
}

// В режиме терминала полосы нет: там кадр терминала, и рисовать поверх
// вывода оболочки нельзя.
func TestNoScrollbarInTerminalMode(t *testing.T) {
	m := newTerminalTui()
	m.width, m.height = 40, 12
	m.term.screen.Resize(40, 11)
	m.term.screen.Write([]byte(strings.Repeat("x", 40) + "\n" + strings.Repeat("line\n", 200)))
	if _, ok := m.scrollbarGeom(m.scrollBodyRows()); ok {
		t.Error("terminal mode must not draw a scrollbar")
	}
}
