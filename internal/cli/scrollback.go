package cli

import (
	"fmt"
	"strings"
)

// Транскрипт живёт в памяти целиком: кадр перерисовывается от строки
// `[dir] …` до статусной строки, и на каждом обновлении он строится заново.
// Лог сборки или тестов приносит десятки тысяч строк, и без предела интерфейс
// со временем встаёт — дороже всего на живом выводе команды.

// maxScrollbackLines — сколько строк транскрипта живёт в памяти.
const maxScrollbackLines = 4000

// countScrollback запоминает, сколько строк добавилось с последней обрезки.
// Пересчитывать весь транскрипт на каждом добавлении нельзя: это O(n) на
// каждый кадр, то есть на каждое слово потока модели.
func (m *tuiModel) countScrollback(lines int) {
	m.scrollbackLines += lines
	if m.scrollbackLines > maxScrollbackLines {
		m.trimScrollback()
	}
}

// appendScrollback дописывает текст в транскрипт и следит за его размером.
func (m *tuiModel) appendScrollback(text string) {
	m.content += text
	m.countScrollback(strings.Count(text, "\n"))
}

// appendLive дописывает кусок вывода фоновой команды. Если пользователь ушёл
// читать выше, scrollOffset растёт вместе с выводом: иначе «сколько строк
// внизу» врало бы и следующий PgDn прыгнул через пол-лога.
func (m *tuiModel) appendLive(text string) {
	if m.scrollOffset > 0 {
		m.scrollOffset += strings.Count(text, "\n")
	}
	m.appendScrollback(text)
}

// trimScrollback оставляет последние строки и ставит наверху пометку о том,
// что было отброшено. Сдвигать scrollOffset не нужно: кадр считается от
// конца транскрипта, и удалённые сверху строки уезжают вместе с ним, так что
// на экране остаётся то же самое.
func (m *tuiModel) trimScrollback() {
	lines := strings.Split(m.content, "\n")
	if len(lines) <= maxScrollbackLines {
		// Счётчик разошёлся с содержимым (например, транскрипт задали
		// извне) — пересчитываем и ждём следующего превышения.
		m.scrollbackLines = len(lines)
		return
	}
	dropped := len(lines) - maxScrollbackLines
	note := fmt.Sprintf("%s… %d lines trimmed from the scrollback%s", colorGray, dropped, colorReset)
	if log := m.auditLogPath(); log != "" {
		note += fmt.Sprintf("%s (full log: %s)%s", colorGray, log, colorReset)
	}
	kept := append([]string{note}, lines[dropped:]...)
	m.content = strings.Join(kept, "\n")
	m.scrollbackLines = len(kept)
}

// auditLogPath — куда пишется полный вывод команд. В подсказке важно дать
// ссылку на него: обрезанный лог иначе выглядит как молчание команды.
func (m tuiModel) auditLogPath() string {
	if m.s == nil {
		return ""
	}
	return m.s.cfg.AuditFile
}

// jumpToEnd возвращает в конец транскрипта: то, что делает колесо вниз.
func (m *tuiModel) jumpToEnd() {
	m.scrollOffset = 0
}
