package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// askStream отправляет запрос модели и стримит вывод полей на экран в реальном времени.
func (s *session) askStream(ctx context.Context, mode, userInput string, out io.Writer) (prompt.Response, string, error) {
	// Один вход в модель для REPL, TUI и one-shot — алиасы раскрываются здесь,
	// чтобы во всех путях запрос уходил в промпт в развёрнутом виде.
	userInput = expandAlias(s.cfg, userInput)
	start := time.Now()
	s.recordRequest(userInput)
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

	s.debugf("request start provider=%s mode=%s tools=%v", s.provider, s.cfg.Mode, s.promptToolNames())
	var err error
	if tc, ok := s.toolEngine(); ok {
		err = s.chatWithTools(ctx, tc, system, user, opts, pr, out)
	} else {
		err = s.streamPlain(ctx, system, user, opts, pr)
	}

	fmt.Fprintln(out)
	dur := time.Since(start)
	s.recordRequestMetrics(pr, dur, err)
	chunks, chars, streamDur, ttft := 0, 0, time.Duration(0), time.Duration(0)
	if pr != nil {
		chunks, chars, streamDur, ttft = pr.metrics()
	}
	s.debugf("request end dur=%s ttft=%s chunks=%d chars=%d stream=%s err=%v", dur, ttft, chunks, chars, streamDur, err)

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
	s.debugf("response parse failed, attempting repair: %v", perr)
	repair := user + "\n\nPrevious response was not valid JSON. Return strictly a single JSON object matching the schema, with no text around it."
	raw2, err := s.engine.Generate(ctx, system, repair, opts)
	if err != nil {
		s.debugf("repair generate failed: %v", err)
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
	out          io.Writer
	raw          strings.Builder
	printed      map[string]int
	headers      map[string]bool
	keys         []string
	startAt      time.Time
	firstTokenAt time.Time
	chunks       int
	chars        int
}

func newStreamPrinter(out io.Writer) *streamPrinter {
	return &streamPrinter{
		out:     out,
		printed: make(map[string]int, 2),
		headers: make(map[string]bool, 2),
		// Question is deliberately not printed here: a clarification is
		// shown by the interactive question flow (TUI / REPL), printing it
		// again would duplicate the text.
		keys:    []string{"command", "explanation"},
		startAt: time.Now(),
	}
}

func (p *streamPrinter) feed(tok string) {
	if tok != "" {
		if p.firstTokenAt.IsZero() {
			p.firstTokenAt = time.Now()
		}
		p.chunks++
		p.chars += len(tok)
	}
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

func (p *streamPrinter) empty() bool {
	return p.printed["command"] == 0 && p.printed["explanation"] == 0
}

func (p *streamPrinter) metrics() (chunks, chars int, streamDur, ttft time.Duration) {
	chunks, chars = p.chunks, p.chars
	if !p.startAt.IsZero() {
		streamDur = time.Since(p.startAt)
	}
	if !p.firstTokenAt.IsZero() && !p.startAt.IsZero() {
		ttft = p.firstTokenAt.Sub(p.startAt)
	}
	return
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
