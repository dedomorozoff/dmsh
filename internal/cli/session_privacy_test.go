package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// История команд — личные данные: файл создаётся закрытым и не дублирует
// записи при повторном вызове.
func TestSaveHistoryIsPrivateAndAppendOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "history.jsonl")
	s := &session{cfg: config.Config{HistoryFile: path}}
	s.hist = []HistoryEntry{
		{Command: "ls", Source: "direct"},
		{Command: "pwd", Source: "llm"},
	}

	if err := s.saveHistory(); err != nil {
		t.Fatalf("saveHistory: %v", err)
	}
	if s.savedHist != 2 {
		t.Fatalf("savedHist = %d, want 2", s.savedHist)
	}
	// Повторный вызов не должен писать те же строки ещё раз.
	if err := s.saveHistory(); err != nil {
		t.Fatalf("second saveHistory: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("history has %d lines, want 2:\n%s", len(lines), data)
	}
	if !strings.Contains(lines[0], `"ls"`) || !strings.Contains(lines[1], `"pwd"`) {
		t.Fatalf("unexpected history content:\n%s", data)
	}
	assertPrivatePerm(t, path)
}

// Аудит-лог создаётся вместе с каталогом и содержит решение политики.
func TestAuditIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "audit.jsonl")
	s := &session{cfg: config.Config{AuditFile: path}}

	s.audit("ls -la", "model", policy.Decision{Allowed: true, Risk: prompt.RiskLow}, executor.Result{})
	s.audit("rm -rf /", "model", policy.Decision{Allowed: false, Risk: prompt.RiskHigh, Reason: "rm -rf на корне"}, executor.Result{})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("audit has %d lines, want 2:\n%s", len(lines), data)
	}
	var entry AuditEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, lines[1])
	}
	if entry.Command != "rm -rf /" || entry.Allowed || entry.Risk != prompt.RiskHigh {
		t.Fatalf("unexpected audit entry: %+v", entry)
	}
	assertPrivatePerm(t, path)
}

// Без настроенного файла запись молча выключается: аудит не должен
// ломать выполнение команд.
func TestAuditWithoutFileIsNoop(t *testing.T) {
	s := &session{}
	s.audit("ls", "direct", policy.Decision{Allowed: true, Risk: prompt.RiskLow}, executor.Result{})
	if err := s.saveHistory(); err != nil {
		t.Fatalf("saveHistory without file must be a no-op: %v", err)
	}
}

// trimHistoryFile оставляет хвост и сохраняет закрытые права.
func TestTrimHistoryFileKeepsPrivatePerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(`{"command":"cmd"}` + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := trimHistoryFile(path, 3); err != nil {
		t.Fatalf("trimHistoryFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n"); len(lines) != 3 {
		t.Fatalf("history has %d lines after trim, want 3", len(lines))
	}
	assertPrivatePerm(t, path)
}

// assertPrivatePerm проверяет права служебного файла. На Windows биты прав
// не отражают ACL, поэтому проверка выполняется только на unix-подобных
// системах.
func assertPrivatePerm(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("%s perm = %o, want 600", path, perm)
	}
}

func TestAuditRedactsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s := &session{cfg: config.Config{AuditFile: path, RemoteAPIKey: "sk-supersecret"}}
	s.audit(`curl -H "Authorization: Bearer sk-supersecret" https://user:pass@example.com`, "model", policy.Decision{Allowed: true, Risk: prompt.RiskLow}, executor.Result{})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "sk-supersecret") || strings.Contains(text, "user:pass@") {
		t.Fatalf("audit leaked secret:\n%s", text)
	}
	var entry AuditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}
	if !strings.Contains(entry.Command, "[REDACTED]") {
		t.Fatalf("expected redacted command, got: %q", entry.Command)
	}
}

func TestHistoryRedactsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := &session{cfg: config.Config{HistoryFile: path, RemoteAPIKey: "sk-supersecret"}}
	s.addHistory("export POLLINATIONS_API_KEY=sk-supersecret", "direct")
	if err := s.saveHistory(); err != nil {
		t.Fatalf("saveHistory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-supersecret") {
		t.Fatalf("history leaked secret:\n%s", data)
	}
}
