package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dedomorozoff/nlsh/internal/config"
	"github.com/dedomorozoff/nlsh/internal/llm"
	"github.com/dedomorozoff/nlsh/internal/model"
	"github.com/dedomorozoff/nlsh/internal/prompt"
)

// HistoryEntry — запись в истории команд.
type HistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Command   string    `json:"command"`
	Source    string    `json:"source"` // "llm" or "direct"
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
	cfg       config.Config
	engine    llm.Engine
	recent    []string
	hist      []HistoryEntry
	input     LineReader
	stats     SessionStats
	lastInput string        // последний запрос пользователя (для /retry)
	turns     []prompt.Turn // последние 5 пар диалога
	stdinCtx  string        // данные из stdin pipe
}

func newSession(cfg config.Config) (*session, error) {
	modelPath, err := resolveModelPath(cfg)
	if err != nil {
		return nil, err
	}
	cfg.ModelPath = modelPath

	eng, err := llm.New(llm.Params{
		ModelPath: cfg.ModelPath,
		Threads:   cfg.Threads,
		CtxSize:   cfg.CtxSize,
		GPULayers: cfg.GPULayers,
	})
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	return &session{
		cfg:    cfg,
		engine: eng,
		stats:  SessionStats{StartTime: time.Now()},
	}, nil
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

	return "", errors.New("model not found, run: nlsh model")
}

func (s *session) close() {
	if s.engine != nil {
		_ = s.engine.Close()
	}
	// Save history
	_ = s.saveHistory()
}

// SetInput sets the line reader for interactive prompts.
func (s *session) SetInput(r LineReader) {
	s.input = r
}

// saveHistory сохраняет историю в файл.
func (s *session) saveHistory() error {
	if s.cfg.HistoryFile == "" {
		return nil
	}

	// Keep only last 1000 entries
	start := 0
	if len(s.hist) > 1000 {
		start = len(s.hist) - 1000
	}
	hist := s.hist[start:]

	// Append to history file
	f, err := os.OpenFile(s.cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	defer func() { _ = f.Close() }()

	for _, entry := range hist {
		data, _ := json.Marshal(entry)
		_, _ = f.WriteString(string(data) + "\n")
	}
	return nil
}

// addHistory добавляет команду в историю сессии.
func (s *session) addHistory(cmd, source string) {
	s.hist = append(s.hist, HistoryEntry{
		Timestamp: time.Now(),
		Command:   cmd,
		Source:    source,
	})
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
	}
	system := prompt.BuildSystem(pctx)
	user := prompt.BuildUser(pctx)

	opts := llm.SamplingOptions{
		MaxTokens:   s.cfg.MaxTokens,
		Temperature: s.cfg.Temperature,
		TopP:        s.cfg.TopP,
		StopTokens:  []string{"<|im_end|>", "</s>"},
	}

	tokens := make(chan string, 128)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.engine.Stream(ctx, system, user, opts, tokens)
	}()

	fmt.Fprintf(out, "%s[nlsh]%s ", cyan, reset)

	var raw strings.Builder
	printedCounts := make(map[string]int)
	headersPrinted := make(map[string]bool)
	keys := []string{"command", "explanation", "question"}

	for tok := range tokens {
		raw.WriteString(tok)
		buf := raw.String()

		for _, k := range keys {
			val, _, _ := getJSONValue(buf, k)
			if val == "" {
				continue
			}

			cleanVal := unescapeJSONString(val)
			alreadyPrinted := printedCounts[k]
			if len(cleanVal) > alreadyPrinted {
				newText := cleanVal[alreadyPrinted:]
				if !headersPrinted[k] {
					headersPrinted[k] = true
					switch k {
					case "command":
						fmt.Fprint(out, "\n\033[36mCommand:\033[0m ")
					case "explanation":
						fmt.Fprint(out, "\n\033[32mExplanation:\033[0m ")
					case "question":
						fmt.Fprint(out, "\n\033[33mQuestion:\033[0m ")
					}
				}
				fmt.Fprint(out, newText)
				if f, ok := out.(*os.File); ok {
					_ = f.Sync()
				}
				printedCounts[k] = len(cleanVal)
			}
		}
	}

	fmt.Fprintln(out)

	if err := <-errCh; err != nil {
		return prompt.Response{}, raw.String(), err
	}

	rawStr := raw.String()
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

	// Выводим восстановленный ответ, так как он не стримился
	if resp2.Command != "" {
		fmt.Fprintf(out, "\n\033[36mCommand:\033[0m %s\n", resp2.Command)
	}
	if resp2.Explanation != "" {
		fmt.Fprintf(out, "\n\033[32mExplanation:\033[0m %s\n", resp2.Explanation)
	}
	if resp2.Question != "" {
		fmt.Fprintf(out, "\n\033[33mQuestion:\033[0m %s\n", resp2.Question)
	}

	return resp2, raw2, nil
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
