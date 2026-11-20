package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Поиск по выводу идёт по видимому тексту, а транскрипт хранит ANSI-цвета.
// Совпадение внутри escape-последовательности («38», «1») подсветило бы
// служебные байты и сломало бы цвет строки, поэтому строка сначала
// разбирается на видимые руны и их позиции в исходной строке.

// containsFold ищет query в text без учёта регистра. Сравнение по рунам, а не
// по байтам: ToLower может менять длину строки, и байтовые индексы разъехались
// бы.
func containsFold(text, query string) bool {
	q := []rune(query)
	if len(q) == 0 {
		return true
	}
	runes := []rune(text)
	if len(runes) < len(q) {
		return false
	}
	for i := 0; i+len(q) <= len(runes); i++ {
		match := true
		for j := range q {
			if unicode.ToLower(runes[i+j]) != unicode.ToLower(q[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// highlightMatches оборачивает совпадения обратным видео. Маркеры вставляются
// вокруг исходных байтов, поэтому цвета строки внутри совпадения не теряются;
// \x1b[27m снимает только инверсию, а не всё оформление.
func highlightMatches(s, query string) string {
	if query == "" {
		return s
	}
	plain, offsets := plainProjection(s)
	q := []rune(query)
	if len(q) == 0 || len(plain) < len(q) {
		return s
	}

	type span struct{ start, end int }
	spans := make([]span, 0, 4)
	for i := 0; i+len(q) <= len(plain); {
		match := true
		for j := range q {
			if unicode.ToLower(plain[i+j]) != unicode.ToLower(q[j]) {
				match = false
				break
			}
		}
		if match {
			spans = append(spans, span{offsets[i], offsets[i+len(q)]})
			i += len(q)
			continue
		}
		i++
	}
	if len(spans) == 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + len(spans)*9)
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp.start])
		b.WriteString("\x1b[7m")
		b.WriteString(s[sp.start:sp.end])
		b.WriteString("\x1b[27m")
		last = sp.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// plainProjection возвращает видимые руны строки и байтовое смещение каждой
// из них в исходной строке. offsets содержит на один элемент больше: так
// конец совпадения можно взять как offsets[len(plain)] без отдельной ветки.
func plainProjection(s string) ([]rune, []int) {
	plain := make([]rune, 0, len(s))
	offsets := make([]int, 0, len(s)+1)
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		plain = append(plain, r)
		offsets = append(offsets, i)
		i += size
	}
	offsets = append(offsets, len(s))
	return plain, offsets
}

// skipEscape возвращает позицию после escape-последовательности. Поддержаны
// CSI (цвета) и OSC (заголовок/гиперссылка); прочие двухбайтовые коды тоже
// пропускаются, чтобы не принять их байты за текст.
func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			j++ // финальный байт последовательности
		}
		return j
	case ']':
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return j
	default:
		return i + 2
	}
}
