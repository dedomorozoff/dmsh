package cli

import (
	"strings"
	"testing"
)

// naiveBodyRows — прежняя реализация layoutRows без кеша. Она остаётся
// эталоном: кеш обязан давать ровно те же строки.
func naiveBodyRows(content string, width int) []string {
	var rows []string
	if content == "" {
		return nil
	}
	body := content
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	raw := strings.Split(body, "\n")
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	for _, l := range raw {
		rows = append(rows, hardWrap(l, width)...)
	}
	return rows
}

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWrappedContentMatchesNaive(t *testing.T) {
	long := strings.Repeat("abcdefghij", 6) // 60 символов: шире узкого экрана
	steps := []string{
		"",
		"первая строка",
		"\n",
		"вторая\n",
		long,
		"\n" + long + "\n",
		"хвост без перевода строки",
		"\n\n\n",
		"ещё\nстрока\n",
	}
	for _, width := range []int{0, 10, 80} {
		m := newTestTui()
		m.width = width
		for i, step := range steps {
			m.appendScrollback(step)
			got := m.wrappedContent()
			want := naiveBodyRows(m.content, width)
			if !equalRows(got, want) {
				t.Fatalf("width=%d step=%d content=%q: got %v, want %v", width, i, m.content, got, want)
			}
		}
	}
}

func TestWrappedContentRebuildsAfterWidthChange(t *testing.T) {
	m := newTestTui()
	m.width = 40
	m.appendScrollback("короткая строка\n" + strings.Repeat("x", 100) + "\n")

	wide := m.wrappedContent()
	if len(wide) != 4 { // строка + 3 переноса от сотни иксов
		t.Fatalf("width=40: %d rows, want 4: %v", len(wide), wide)
	}

	m.width = 200
	narrow := m.wrappedContent()
	if !equalRows(narrow, naiveBodyRows(m.content, 200)) {
		t.Fatalf("width change not rebuilt: %v", narrow)
	}
	if len(narrow) != 2 {
		t.Fatalf("width=200: %d rows, want 2: %v", len(narrow), narrow)
	}
}

func TestWrappedContentRebuildsAfterTrim(t *testing.T) {
	m := newTestTui()
	m.width = 80
	half := maxScrollbackLines / 2
	m.appendScrollback(strings.Repeat("строка\n", half))
	m.wrappedContent()

	// Ещё столько же строк переполняет предел и обрезает начало транскрипта:
	// кеш обязан заметить, что старое содержимое больше не префикс.
	m.appendScrollback(strings.Repeat("ещё\n", half))
	if !equalRows(m.wrappedContent(), naiveBodyRows(m.content, 80)) {
		t.Fatal("cache survived a trimmed transcript")
	}
}

func TestWrappedContentRebuildsAfterRewrite(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.appendScrollback("первая\nвторая\nтретья\n")
	m.wrappedContent()

	m.content = strings.Replace(m.content, "вторая", "ДРУГАЯ", 1)
	if !equalRows(m.wrappedContent(), naiveBodyRows(m.content, 80)) {
		t.Fatal("cache survived a rewritten transcript")
	}
}

func TestWrappedContentHighlightCacheInvalidation(t *testing.T) {
	m := newTestTui()
	m.width = 80
	m.appendScrollback("alpha\nbeta\n")

	if rows := m.wrappedContent(); strings.Contains(strings.Join(rows, "\n"), "\x1b[7m") {
		t.Fatal("nothing should be highlighted without a query")
	}

	m.findQuery = "alpha"
	rows := m.wrappedContent()
	if !strings.Contains(strings.Join(rows, "\n"), "\x1b[7malpha\x1b[27m") {
		t.Fatalf("query change did not rebuild the rows: %q", rows)
	}

	// Дописывание идёт по инкрементальному пути: новая строка тоже должна
	// быть подсвечена, а старые маркеры — остаться на месте.
	m.appendScrollback("alpha again\n")
	rows = m.wrappedContent()
	if strings.Count(strings.Join(rows, "\n"), "\x1b[7m") != 2 {
		t.Fatalf("appended line was not highlighted: %q", rows)
	}

	m.findQuery = ""
	rows = m.wrappedContent()
	if strings.Contains(strings.Join(rows, "\n"), "\x1b[7m") {
		t.Fatal("clearing the query should remove the highlight")
	}
	if !equalRows(rows, naiveBodyRows(m.content, 80)) {
		t.Fatal("unhighlighted rows differ from the plain transcript")
	}
}
