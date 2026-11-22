package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// HistoryEntry — запись в истории команд.
type HistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Command   string    `json:"command"`
	Source    string    `json:"source"` // "llm", "tool" or "direct"
}

// AuditEntry — строка аудит-лога: что реально выполнялось и какое решение
// политики было принято. Ведётся для отчётности и разбора инцидентов.
type AuditEntry struct {
	Timestamp time.Time   `json:"timestamp"`
	Command   string      `json:"command"`
	Source    string      `json:"source"` // "llm", "tool" or "direct"
	Risk      prompt.Risk `json:"risk"`
	Allowed   bool        `json:"allowed"`
	Reason    string      `json:"reason,omitempty"`
	ExitCode  int         `json:"exit_code,omitempty"`
	Success   bool        `json:"success"`
}

// SessionStats — статистика текущей сессии.
type SessionStats struct {
	StartTime       time.Time
	Requests        int
	CommandsLLM     int
	CommandsDirect  int
	ErrorsFix       int
	ToolSteps       int
	RequestErrors   int
	StreamChunks    int
	StreamChars     int
	TotalStreamTime time.Duration
	TotalTTFT       time.Duration
	TTFTSamples     int
	LastRequestDur  time.Duration
	LastTTFT        time.Duration
}

// session инкапсулирует движок и собранный для него контекст промпта.
// Один session живёт всё время REPL или одной CLI-команды.
//
// mu защищает состояние, к которому пишет горутина стрима (askStream),
// пока UI читает и правит его же: отмена запроса по Esc не ждёт завершения
// askStream, поэтому новый запрос может пересечься со старым. Это же делает
// showStats безопасным во время стрима.
type session struct {
	mu       sync.Mutex
	cfg      config.Config
	provider config.Provider
	notice   string
	engine   llm.Engine
	tools    *tools.Registry
	todos    *tools.TodoStore
	plugins  map[string]tools.Plugin // tool name -> allowlisted executable
	// searchEngine обслуживает инструмент websearch. Он создаётся лениво и
	// использует отдельную search-модель того же провайдера.
	searchEngine llm.Engine
	recent       []string
	hist         []HistoryEntry
	savedHist    int // индекс первой незаписанной записи в hist
	input        LineReader
	stats        SessionStats
	lastInput    string        // последний запрос пользователя
	turns        []prompt.Turn // последние 5 пар диалога
	stdinCtx     string        // данные из stdin pipe
	autoYes      bool          // --yes: пропускать все подтверждения
	debug        *debugLogger
	sessionName  string
}

// recordRequest запоминает запрос пользователя и увеличивает счётчик.
// Вызывается из горутины стрима, поэтому под mu.
func (s *session) recordRequest(input string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Requests++
	s.lastInput = input
}

func (s *session) recordRequestMetrics(pr *streamPrinter, dur time.Duration, err error) {
	var chunks, chars int
	var streamDur, ttft time.Duration
	if pr != nil {
		chunks, chars, streamDur, ttft = pr.metrics()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.LastRequestDur = dur
	s.stats.LastTTFT = ttft
	s.stats.StreamChunks += chunks
	s.stats.StreamChars += chars
	s.stats.TotalStreamTime += streamDur
	if ttft > 0 {
		s.stats.TotalTTFT += ttft
		s.stats.TTFTSamples++
	}
	if err != nil {
		s.stats.RequestErrors++
	}
}

func (s *session) recordToolStep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.ToolSteps++
}

func (s *session) statsSnapshot() SessionStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *session) setupDebug(enabled bool, path string) error {
	if !enabled {
		return nil
	}
	l, err := newDebugLogger(true, path, s.cfg.APIKey(), s.cfg.RemoteAPIKey, s.cfg.ProxyPassword)
	if err != nil {
		return err
	}
	s.debug = l
	s.debugf("session provider=%s model=%s remote=%s proxy=%v", s.provider, s.cfg.RemoteModel, s.cfg.RemoteBaseURL, s.cfg.Proxy().Mode)
	return nil
}

func (s *session) debugf(format string, args ...any) {
	if s == nil || s.debug == nil {
		return
	}
	s.debug.printf(format, args...)
}

func (s *session) setSessionName(name string) {
	s.sessionName = sanitizeSessionName(name)
	s.turns = nil
	s.loadTurns()
	if s.sessionName != "" {
		s.debugf("session name=%s turns=%d", s.sessionName, len(s.turns))
	}
}

// lastRequest возвращает последний запрос пользователя (/retry, уточнения).
func (s *session) lastRequest() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastInput
}

// addFixNote считает автокоррекцию: пишет из UI, читает /stats.
func (s *session) addFixNote() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.ErrorsFix++
}

// newToolRegistry собирает набор инструментов для текущей сессии.
// Команда оболочки попадает в схемы, но исполняется отдельно — с проверкой
// политики безопасности, поэтому её обработчик в реестре — заглушка.
// Файловые инструменты ограничены рабочим каталогом, домашним каталогом
// и временной папкой, чтобы модель не читала произвольные файлы системы.
func newToolRegistry(shell string) *tools.Registry {
	r := tools.NewRegistry().Restrict(tools.DefaultRoots()...)
	r.Register(tools.RunCommandSpec(shell), func(context.Context, string) (string, error) {
		return "", errors.New("run_command must go through the dmsh security policy")
	})
	return r
}

// normalizeForProvider приводит конфиг к уже выбранному провайдеру: пустая
// модель означает «дефолт провайдера», а локальному GGUF адрес и имя модели
// не нужны. Флаг --provider может переключить провайдера поверх модели,
// взятой из конфига, поэтому дефолт выбирается здесь, а не в Load.
func normalizeForProvider(cfg config.Config, provider config.Provider) (config.Config, error) {
	cfg.Provider = provider
	if !provider.Remote() {
		path, err := resolveModelPath(cfg)
		if err != nil {
			return cfg, err
		}
		cfg.ModelPath = path
		return cfg, nil
	}
	cfg.ModelPath = ""
	if strings.TrimSpace(cfg.RemoteModel) == "" {
		cfg.RemoteModel = config.DefaultModelFor(provider)
	}
	return cfg, nil
}

func newSession(cfg config.Config) (*session, error) {
	provider, notice, err := resolveProvider(cfg)
	if err != nil {
		return nil, err
	}
	cfg, err = normalizeForProvider(cfg, provider)
	if err != nil {
		return nil, err
	}

	s := &session{
		cfg:      cfg,
		provider: provider,
		notice:   notice,
		tools:    newToolRegistry(cfg.Shell),
		todos:    tools.NewTodoStore(),
		plugins:  discoveredPlugins(cfg.PluginAllowlist),
		stats:    SessionStats{StartTime: time.Now()},
	}

	s.registerOptionalTools()

	eng, err := s.newEngine()
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	s.engine = eng
	s.loadTurns()
	return s, nil
}

// SetInput sets the line reader for interactive prompts.
func (s *session) SetInput(r LineReader) {
	s.input = r
}

// setAutoYes включает режим --yes: все подтверждения выполняются автоматически.
func (s *session) setAutoYes(v bool) {
	s.autoYes = v
}

// confirmOK спрашивает подтверждение, если не включён --yes.
func (s *session) confirmOK(out io.Writer, promptText string) (bool, error) {
	if s.autoYes {
		return true, nil
	}
	return confirm(s.input, out, s, promptText)
}
