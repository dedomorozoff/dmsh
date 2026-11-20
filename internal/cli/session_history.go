package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/fsutil"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/redact"
)

// historyEntries возвращает историю для показа: сначала она уже накоплена в
// этой сессии, а текущая сессия ещё не сброшена в файл. Поэтому записи
// текущей сессии дописываются в конец файловых, иначе /history терял бы всё,
// что сделано в dmsh прямо сейчас.
func (s *session) historyEntries() []HistoryEntry {
	fileEntries, err := loadHistoryEntries(s.cfg.HistoryFile)
	if err != nil {
		fileEntries = nil
	}
	return append(fileEntries, s.hist[s.savedHist:]...)
}

// saveHistory дописывает в файл только новые (ещё не сохранённые) записи.
// Это предотвращает дублирование при повторном вызове close() и позволяет
// файлу расти постепенно. Обрезка до 1000 строк выполняется раз в 500 новых
// записей, чтобы файл не рос бесконечно.
func (s *session) saveHistory() error {
	if s.cfg.HistoryFile == "" {
		return nil
	}
	newEntries := s.hist[s.savedHist:]
	if len(newEntries) == 0 {
		return nil
	}

	f, err := fsutil.AppendPrivate(s.cfg.HistoryFile)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	for _, entry := range newEntries {
		data, _ := json.Marshal(entry)
		_, _ = f.WriteString(string(data) + "\n")
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close history file: %w", closeErr)
	}
	s.savedHist = len(s.hist)

	// Обрезаем файл до 1000 строк каждые 500 новых записей.
	if len(newEntries) >= 500 {
		_ = trimHistoryFile(s.cfg.HistoryFile, 1000)
	}
	return nil
}

// trimHistoryFile оставляет в файле только последние maxLines строк,
// атомарно заменяя исходный файл через временный.
func trimHistoryFile(path string, maxLines int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= maxLines {
		return nil
	}
	lines = lines[len(lines)-maxLines:]
	tmp := path + ".tmp"
	if err := fsutil.WritePrivate(tmp, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// addHistory добавляет команду в историю сессии.
func (s *session) redactSecrets(text string) string {
	if s == nil {
		return redact.Text(text)
	}
	return redact.Text(text, s.cfg.APIKey(), s.cfg.RemoteAPIKey, s.cfg.ProxyPassword)
}

func (s *session) addHistory(cmd, source string) {
	s.hist = append(s.hist, HistoryEntry{
		Timestamp: time.Now(),
		Command:   s.redactSecrets(cmd),
		Source:    source,
	})
}

// audit пишет строку в файл аудит-лога (одна строка JSON). Ошибки записи
// игнорируются: аудит не должен ломать выполнение команд.
func (s *session) audit(cmd, source string, dec policy.Decision, res executor.Result) {
	if s.cfg.AuditFile == "" {
		return
	}
	entry := AuditEntry{
		Timestamp: time.Now(),
		Command:   s.redactSecrets(cmd),
		Source:    source,
		Risk:      dec.Risk,
		Allowed:   dec.Allowed,
		Reason:    s.redactSecrets(dec.Reason),
		ExitCode:  res.ExitCode,
		Success:   res.Err == nil && res.ExitCode == 0,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := fsutil.AppendPrivate(s.cfg.AuditFile)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(string(data) + "\n")
}

func (s *session) addRecent(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}
	s.recent = append(s.recent, cmd)
	if len(s.recent) > 10 {
		s.recent = s.recent[len(s.recent)-10:]
	}
}

// addRecentAndHistory добавляет команду в recent и историю.
func (s *session) addRecentAndHistory(cmd, source string) {
	s.addRecent(cmd)
	s.addHistory(cmd, source)
	s.mu.Lock()
	defer s.mu.Unlock()
	if source == "llm" {
		s.stats.CommandsLLM++
	} else {
		s.stats.CommandsDirect++
	}
}

// addTurn добавляет пару диалога (последние 5 пар). Пишет горутина стрима,
// а читает askStream при сборке следующего промпта.
func (s *session) addTurn(user, assistant string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, prompt.Turn{User: user, Assistant: assistant})
	if len(s.turns) > 5 {
		s.turns = s.turns[len(s.turns)-5:]
	}
}

// sessionFile возвращает путь к файлу персиста multi-turn контекста.
// sessionFile возвращает путь к файлу истории multi-turn контекста.
func sessionFile() string {
	return sessionFileNamed("")
}

func sessionFileNamed(name string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	name = sanitizeSessionName(name)
	if name == "" {
		return filepath.Join(dir, "dmsh", "session.json")
	}
	return filepath.Join(dir, "dmsh", "sessions", name+".json")
}

func sanitizeSessionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return ""
	}
	return out
}

// loadTurns восстанавливает multi-turn контекст из файла, если включён
// ResumeSession или явно задано имя сессии. Ошибки чтения/парсинга
// игнорируются - это не обязательный файл.
func (s *session) loadTurns() {
	if !s.cfg.ResumeSession && s.sessionName == "" {
		return
	}
	path := sessionFileNamed(s.sessionName)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var turns []prompt.Turn
	if json.Unmarshal(data, &turns) != nil {
		return
	}
	if len(turns) > 5 {
		turns = turns[len(turns)-5:]
	}
	s.turns = turns
}

// saveTurns сохраняет multi-turn контекст по завершении сессии.
func (s *session) saveTurns() {
	if !s.cfg.ResumeSession && s.sessionName == "" {
		return
	}
	path := sessionFileNamed(s.sessionName)
	if path == "" {
		return
	}
	data, err := json.Marshal(s.turns)
	if err != nil {
		return
	}
	if err := fsutil.WritePrivate(path, data); err != nil {
		return
	}
}
