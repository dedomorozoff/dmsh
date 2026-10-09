package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/dedomorozoff/dmsh/internal/policy"
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
	StartTime      time.Time
	Requests       int
	CommandsLLM    int
	CommandsDirect int
	ErrorsFix      int
}

// session инкапсулирует движок и собранный для него контекст промпта.
// Один session живёт всё время REPL или одной CLI-команды.
type session struct {
	cfg      config.Config
	provider config.Provider
	notice   string
	engine   llm.Engine
	tools    *tools.Registry
	todos    *tools.TodoStore
	// searchEngine обслуживает инструмент websearch. Он создаётся лениво и
	// использует отдельную search-модель того же провайдера.
	searchEngine llm.Engine
	recent       []string
	hist         []HistoryEntry
	savedHist    int // индекс первой незаписанной записи в hist
	input        LineReader
	stats        SessionStats
	lastInput    string        // последний запрос пользователя (для /retry)
	retryPending bool          // /retry был введён — повторить lastInput
	turns        []prompt.Turn // последние 5 пар диалога
	stdinCtx     string        // данные из stdin pipe
	autoYes      bool          // --yes: пропускать все подтверждения
}

// newToolRegistry собирает набор инструментов для текущей сессии.
// Команда оболочки попадает в схемы, но исполняется отдельно — с проверкой
// политики безопасности, поэтому её обработчик в реестре — заглушка.
func newToolRegistry(shell string) *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.RunCommandSpec(shell), func(context.Context, string) (string, error) {
		return "", errors.New("run_command must go through the dmsh security policy")
	})
	return r
}

func newSession(cfg config.Config) (*session, error) {
	provider, notice, err := resolveProvider(cfg)
	if err != nil {
		return nil, err
	}
	cfg.Provider = provider

	s := &session{
		cfg:      cfg,
		provider: provider,
		notice:   notice,
		tools:    newToolRegistry(cfg.Shell),
		todos:    tools.NewTodoStore(),
		stats:    SessionStats{StartTime: time.Now()},
	}

	s.registerOptionalTools()

	if provider == config.ProviderLocal {
		modelPath, err := resolveModelPath(cfg)
		if err != nil {
			return nil, err
		}
		s.cfg.ModelPath = modelPath
	} else {
		s.cfg.ModelPath = ""
	}

	eng, err := s.newEngine()
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	s.engine = eng
	s.loadTurns()
	return s, nil
}

// newEngine создаёт движок для текущего провайдера и конфигурации.
func (s *session) newEngine() (llm.Engine, error) {
	return llm.New(llm.Params{
		Provider:      toLLMProvider(s.provider),
		ModelPath:     s.cfg.ModelPath,
		Threads:       s.cfg.Threads,
		CtxSize:       s.cfg.CtxSize,
		GPULayers:     s.cfg.GPULayers,
		RemoteModel:   s.cfg.RemoteModel,
		RemoteBaseURL: s.cfg.RemoteBaseURL,
	})
}

// toLLMProvider переводит провайдера конфига в тип движка.
func toLLMProvider(p config.Provider) llm.Provider {
	switch p {
	case config.ProviderPollinations:
		return llm.ProviderPollinations
	case config.ProviderLocal:
		return llm.ProviderLocal
	default:
		return llm.ProviderAuto
	}
}

// resolveProvider выбирает провайдера инференса. Для ProviderAuto локальный
// GGUF используется только если файл действительно найден, иначе
// включается Pollinations и возвращается notice для показа пользователю.
// Скрытого отката при ошибке локального инференса нет: выбор делается
// один раз, здесь.
func resolveProvider(cfg config.Config) (config.Provider, string, error) {
	switch cfg.Provider {
	case config.ProviderPollinations:
		return config.ProviderPollinations, "", nil
	case config.ProviderLocal:
		if _, err := resolveModelPath(cfg); err != nil {
			return "", "", fmt.Errorf("provider=local, but no GGUF model found: %w\n"+
				"  download one:   dmsh model download\n"+
				"  or go remote:   dmsh config set provider auto", err)
		}
		return config.ProviderLocal, "", nil
	case "", config.ProviderAuto:
		if _, err := resolveModelPath(cfg); err == nil {
			return config.ProviderLocal, "", nil
		}
		return config.ProviderPollinations,
			"no local GGUF model found — using Pollinations (provider=auto)", nil
	default:
		return "", "", fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

// printNotice показывает предупреждение о выбранном провайдере, если оно есть.
func (s *session) printNotice(out io.Writer) {
	if s.notice == "" {
		return
	}
	fmt.Fprintf(out, "%s[dmsh]%s %s%s%s\n", yellow, reset, yellow, s.notice, reset)
}

func resolveModelPath(cfg config.Config) (string, error) {
	if cfg.ModelPath != "" {
		if _, err := os.Stat(cfg.ModelPath); err == nil {
			return cfg.ModelPath, nil
		}
		d := model.New("")
		if d.Exists(cfg.ModelPath) {
			return d.ModelPath(cfg.ModelPath), nil
		}
	}

	if cfg.DefaultModel != "" {
		d := model.New("")
		if d.Exists(cfg.DefaultModel) {
			return d.ModelPath(cfg.DefaultModel), nil
		}
	}

	d := model.New("")
	available := d.ListModels()
	if len(available) > 0 {
		return d.ModelPath(available[0].Name), nil
	}

	all, _ := d.ListAllModels()
	if len(all) > 0 {
		return d.ModelPath(all[0].Name), nil
	}

	return "", errors.New("model not found, run: dmsh model")
}

func (s *session) close() {
	if s.engine != nil {
		_ = s.engine.Close()
	}
	if s.searchEngine != nil {
		_ = s.searchEngine.Close()
		s.searchEngine = nil
	}
	// Save history
	_ = s.saveHistory()
	s.saveTurns()
}

// switchModel заменяет загруженную модель на лету. При ошибке загрузки
// старая модель остаётся на месте и продолжает работать. Переключение на
// локальную модель всегда переводит сессию на провайдер local.
func (s *session) switchModel(path string) error {
	if s.engine == nil {
		return errors.New("no engine loaded")
	}
	eng, err := llm.New(llm.Params{
		Provider:  llm.ProviderLocal,
		ModelPath: path,
		Threads:   s.cfg.Threads,
		CtxSize:   s.cfg.CtxSize,
		GPULayers: s.cfg.GPULayers,
	})
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	// Закрываем старый движок только после успешной загрузки нового,
	// чтобы не оставить s.engine указывающим на закрытый объект.
	old := s.engine
	s.engine = eng
	s.cfg.ModelPath = path
	s.provider = config.ProviderLocal
	s.cfg.Provider = config.ProviderLocal
	s.notice = ""
	_ = old.Close()
	return nil
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

	f, err := os.OpenFile(s.cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// addHistory добавляет команду в историю сессии.
func (s *session) addHistory(cmd, source string) {
	s.hist = append(s.hist, HistoryEntry{
		Timestamp: time.Now(),
		Command:   cmd,
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
		Command:   cmd,
		Source:    source,
		Risk:      dec.Risk,
		Allowed:   dec.Allowed,
		Reason:    dec.Reason,
		ExitCode:  res.ExitCode,
		Success:   res.Err == nil && res.ExitCode == 0,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.cfg.AuditFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
	if source == "llm" {
		s.stats.CommandsLLM++
	} else {
		s.stats.CommandsDirect++
	}
}

// addTurn добавляет пару диалога (последние 5 пар).
func (s *session) addTurn(user, assistant string) {
	s.turns = append(s.turns, prompt.Turn{User: user, Assistant: assistant})
	if len(s.turns) > 5 {
		s.turns = s.turns[len(s.turns)-5:]
	}
}

// sessionFile возвращает путь к файлу персиста multi-turn контекста.
func sessionFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "dmsh", "session.json")
}

// loadTurns восстанавливает multi-turn контекст из файла, если включён
// ResumeSession. Ошибки чтения/парсинга молча игнорируются — это лишь
// подсказка для модели, а не обязательные данные.
func (s *session) loadTurns() {
	if !s.cfg.ResumeSession {
		return
	}
	path := sessionFile()
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

// saveTurns персистит multi-turn контекст при завершении сессии.
func (s *session) saveTurns() {
	if !s.cfg.ResumeSession {
		return
	}
	path := sessionFile()
	if path == "" {
		return
	}
	data, err := json.Marshal(s.turns)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

// askStream отправляет запрос модели и стримит вывод полей на экран в реальном времени.
func (s *session) askStream(ctx context.Context, mode, userInput string, out io.Writer) (prompt.Response, string, error) {
	s.stats.Requests++
	s.lastInput = userInput
	cwd, _ := os.Getwd()
	pctx := prompt.Context{
		OS:           osName(),
		OSVersion:    osVersion(),
		BuildInfo:    buildInfo(),
		Shell:        s.cfg.Shell,
		CWD:          cwd,
		RecentCmds:   s.recent,
		UserRequest:  userInput,
		Mode:         string(s.cfg.Mode),
		StdinContext: s.stdinCtx,
		RecentTurns:  s.turns,
		Tools:        s.promptToolNames(),
	}
	system := prompt.BuildSystem(pctx)
	user := prompt.BuildUser(pctx)

	opts := llm.SamplingOptions{
		MaxTokens:   s.cfg.MaxTokens,
		Temperature: s.cfg.Temperature,
		TopP:        s.cfg.TopP,
		StopTokens:  []string{"<|im_end|>", "</s>"},
	}

	pr := newStreamPrinter(out)
	fmt.Fprintf(out, "%s[dmsh]%s ", cyan, reset)

	var err error
	if tc, ok := s.toolEngine(); ok {
		err = s.chatWithTools(ctx, tc, system, user, opts, pr, out)
	} else {
		err = s.streamPlain(ctx, system, user, opts, pr)
	}

	fmt.Fprintln(out)

	if err != nil {
		return prompt.Response{}, pr.String(), err
	}

	return s.finishAnswer(ctx, system, user, opts, pr, out, userInput)
}

// streamPlain — обычный запрос без инструментов: токены печатаются
// по мере генерации.
func (s *session) streamPlain(ctx context.Context, system, user string, opts llm.SamplingOptions, pr *streamPrinter) error {
	tokens := make(chan string, 128)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.engine.Stream(ctx, system, user, opts, tokens)
	}()

	for tok := range tokens {
		pr.feed(tok)
	}
	return <-errCh
}

// finishAnswer разбирает накопленный JSON-ответ и, если модель его сломала,
// просит починить (как и раньше).
func (s *session) finishAnswer(ctx context.Context, system, user string, opts llm.SamplingOptions, pr *streamPrinter, out io.Writer, userInput string) (prompt.Response, string, error) {
	rawStr := pr.String()
	resp, perr := prompt.Parse(rawStr)
	if perr == nil {
		// Сохраняем пару диалога для multi-turn контекста
		asst := resp.Command
		if asst == "" {
			asst = resp.Explanation
		}
		s.addTurn(userInput, asst)
		return resp, rawStr, nil
	}

	// Ремонт JSON при неудаче
	repair := user + "\n\nPrevious response was not valid JSON. Return strictly a single JSON object matching the schema, with no text around it."
	raw2, err := s.engine.Generate(ctx, system, repair, opts)
	if err != nil {
		return prompt.Response{}, rawStr, err
	}
	resp2, perr2 := prompt.Parse(raw2)
	if perr2 != nil {
		return prompt.Response{}, rawStr + "\n---\n" + raw2, fmt.Errorf("failed to parse model response: %w", perr2)
	}

	// Поля первого ответа уже были отрисованы при стриминге, поэтому не
	// дублируем их. Если ремонт вернул контент там, где стрим был пуст,
	// печатаем только его.
	if pr.empty() {
		if resp2.Command != "" {
			fmt.Fprintf(out, "\n\033[36mCommand:\033[0m %s\n", resp2.Command)
		}
		if resp2.Explanation != "" {
			fmt.Fprintf(out, "\n\033[32mExplanation:\033[0m %s\n", resp2.Explanation)
		}
	} else {
		fmt.Fprintf(out, "%s(reformatted)%s\n", gray, reset)
	}

	return resp2, raw2, nil
}

// streamPrinter копит поток ответа модели и печатает значения полей JSON
// по мере их появления, не дожидаясь закрывающей скобки.
type streamPrinter struct {
	out     io.Writer
	raw     strings.Builder
	printed map[string]int
	headers map[string]bool
	keys    []string
}

func newStreamPrinter(out io.Writer) *streamPrinter {
	return &streamPrinter{
		out:     out,
		printed: make(map[string]int, 2),
		headers: make(map[string]bool, 2),
		// Question is deliberately not printed here: a clarification is
		// shown by the interactive question flow (TUI / REPL), printing it
		// again would duplicate the text.
		keys: []string{"command", "explanation"},
	}
}

func (p *streamPrinter) feed(tok string) {
	p.raw.WriteString(tok)
	buf := p.raw.String()

	for _, k := range p.keys {
		val, _, _ := getJSONValue(buf, k)
		if val == "" {
			continue
		}
		cleanVal := unescapeJSONString(val)
		already := p.printed[k]
		if len(cleanVal) <= already {
			continue
		}
		if !p.headers[k] {
			p.headers[k] = true
			switch k {
			case "command":
				fmt.Fprint(p.out, "\n\033[36mCommand:\033[0m ")
			case "explanation":
				fmt.Fprint(p.out, "\n\033[32mExplanation:\033[0m ")
			case "question":
				fmt.Fprint(p.out, "\n\033[33mQuestion:\033[0m ")
			}
		}
		fmt.Fprint(p.out, cleanVal[already:])
		if f, ok := p.out.(*os.File); ok {
			_ = f.Sync()
		}
		p.printed[k] = len(cleanVal)
	}
}

func (p *streamPrinter) String() string { return p.raw.String() }

// empty сообщает, что ни одно поле не было напечатано — по этому признаку
// решается, печатать ли отремонтированный ответ целиком.
func (p *streamPrinter) empty() bool {
	return p.printed["command"] == 0 && p.printed["explanation"] == 0
}

func getJSONValue(buf, key string) (value string, hasClosed bool, startIdx int) {
	keyIdx := strings.Index(buf, `"`+key+`"`)
	if keyIdx == -1 {
		return "", false, -1
	}

	colonIdx := strings.IndexByte(buf[keyIdx:], ':')
	if colonIdx == -1 {
		return "", false, -1
	}
	colonIdx += keyIdx

	quoteIdx := strings.IndexByte(buf[colonIdx:], '"')
	if quoteIdx == -1 {
		return "", false, -1
	}
	quoteIdx += colonIdx
	startIdx = quoteIdx + 1

	if startIdx >= len(buf) {
		return "", false, startIdx
	}

	escaped := false
	for i := startIdx; i < len(buf); i++ {
		c := buf[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return buf[startIdx:i], true, startIdx
		}
	}
	return buf[startIdx:], false, startIdx
}

func unescapeJSONString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			switch c {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			default:
				sb.WriteByte('\\')
				sb.WriteByte(c)
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		sb.WriteByte(c)
	}
	if escaped {
		sb.WriteByte('\\')
	}
	return sb.String()
}
