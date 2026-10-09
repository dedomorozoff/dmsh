package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/policy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// maxToolSteps ограничивает длину цепочки вызовов инструментов: защита от
// situations, когда модель ходит по кругу и тратит токены впустую.
const maxToolSteps = 12

// toolEngine возвращает движок, если инструменты разрешены и он их умеет.
// Локальный llama.cpp без постобработки tools не поддерживает, поэтому
// запрос идёт обычным путём.
func (s *session) toolEngine() (llm.ToolEngine, bool) {
	if !s.cfg.ToolsEnabled {
		return nil, false
	}
	tc, ok := s.engine.(llm.ToolEngine)
	if !ok {
		return nil, false
	}
	return tc, true
}

// chatWithTools выполняет диалог с вызовами инструментов. Пока модель
// просит инструменты, она получает результаты их работы; когда вызовов
// больше нет, накопленный текст разбирается как обычный JSON-контракт.
func (s *session) chatWithTools(
	ctx context.Context,
	tc llm.ToolEngine,
	system, user string,
	opts llm.SamplingOptions,
	pr *streamPrinter,
	out io.Writer,
) error {
	specs := s.tools.Specs()
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}

	for step := 0; step < maxToolSteps; step++ {
		msg, err := s.chatStep(ctx, tc, msgs, specs, opts, pr)
		if err != nil {
			return err
		}
		if len(msg.ToolCalls) == 0 {
			return nil
		}

		msgs = append(msgs, msg)
		for _, call := range msg.ToolCalls {
			fmt.Fprintf(out, "\n%s[tool]%s %s%s%s\n", cyan, reset, cyan, call.Name, reset)
			msgs = append(msgs, llm.NewToolResult(call, s.runToolCall(ctx, call, out)))
		}
	}
	return fmt.Errorf("tool call limit reached after %d steps", maxToolSteps)
}

// chatStep делает один запрос к модели и параллельно печатает приходящие
// дельты текста. Дельты читаются до возврата Chat: иначе движок упёрся бы
// в буфер канала и не смог бы дописать остаток ответа.
func (s *session) chatStep(
	ctx context.Context,
	tc llm.ToolEngine,
	msgs []llm.Message,
	specs []llm.ToolSpec,
	opts llm.SamplingOptions,
	pr *streamPrinter,
) (llm.Message, error) {
	type outcome struct {
		msg llm.Message
		err error
	}

	deltas := make(chan string, 64)
	done := make(chan outcome, 1)

	go func() {
		msg, err := tc.Chat(ctx, llm.ChatRequest{
			Messages: msgs,
			Tools:    specs,
			Options:  opts,
			Deltas:   deltas,
		})
		done <- outcome{msg: msg, err: err}
	}()

	// Реализация ToolEngine обязана закрыть deltas перед возвратом.
	for d := range deltas {
		pr.feed(d)
	}
	res := <-done
	if res.err != nil {
		return llm.Message{}, res.err
	}
	if res.msg.Role == "" {
		res.msg.Role = llm.RoleAssistant
	}
	return res.msg, nil
}

// runToolCall выполняет инструмент, выбранный моделью. Результат всегда
// возвращается текстом, а не ошибкой: модель должна увидеть отказ и
// попробовать другой путь.
func (s *session) runToolCall(ctx context.Context, call llm.ToolCall, out io.Writer) string {
	switch call.Name {
	case tools.NameRunCommand:
		return s.runCommandTool(ctx, call, out)
	case tools.NameAsk:
		return s.runAskTool(call, out)
	case tools.NameWebSearch:
		return s.runWebSearchTool(ctx, call)
	default:
		return s.tools.Call(ctx, call.Name, call.Arguments)
	}
}

// runAskTool задаёт пользователю один вопрос и ждёт ответа. В TUI ввода
// нет — там модель должна задать вопрос сама, через intent=ask_clarification.
func (s *session) runAskTool(call llm.ToolCall, out io.Writer) string {
	var args tools.AskArgs
	if err := call.DecodeArgs(&args); err != nil {
		return tools.EncodeResult(tools.Result{OK: false, Error: "invalid arguments: " + err.Error()})
	}
	question := strings.TrimSpace(args.Question)
	if question == "" {
		return tools.EncodeResult(tools.Result{OK: false, Error: "question is required"})
	}
	if s.input == nil {
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "the user cannot be asked mid-task in this UI: return intent=ask_clarification with this question instead",
		})
	}

	fmt.Fprintf(out, "\n%s[ask]%s %s%s%s\n", cyan, reset, cyan, question, reset)
	for _, choice := range args.Choices {
		choice = strings.TrimSpace(choice)
		if choice == "" {
			continue
		}
		fmt.Fprintf(out, "  %s-%s %s\n", gray, reset, choice)
	}
	flushOutput(out)

	answer, err := s.input.ReadLine()
	if err != nil {
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "no answer received: " + err.Error() + ". Proceed with the most sensible default instead.",
		})
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "the user answered nothing. Proceed with the most sensible default instead.",
		})
	}
	if strings.HasPrefix(answer, "/") {
		// Ответ вида "/var/log" — обычный текст, а не команда: слэш-команды
		// во время выполнения инструментов не обрабатываются.
		fmt.Fprintf(out, "%s[ask]%s %s%s%s\n", gray, reset, gray, answer, reset)
	}
	return tools.EncodeResult(tools.Result{OK: true, Content: answer})
}

// searchTimeout ограничивает один веб-поиск: поиск — вспомогательная
// операция, ждать его вечно не нужно.
const searchTimeout = 60 * time.Second

// runWebSearchTool ищет в вебе через search-модель того же провайдера.
// Отдельный движок создаётся лениво: обычный движок занят текущим запросом.
func (s *session) runWebSearchTool(ctx context.Context, call llm.ToolCall) string {
	var args tools.WebSearchArgs
	if err := call.DecodeArgs(&args); err != nil {
		return tools.EncodeResult(tools.Result{OK: false, Error: "invalid arguments: " + err.Error()})
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return tools.EncodeResult(tools.Result{OK: false, Error: "query is required"})
	}
	if s.provider != config.ProviderPollinations {
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "websearch is only available for the remote provider; answer from local sources instead",
		})
	}
	eng, err := s.searchEngineFor()
	if err != nil {
		return tools.EncodeResult(tools.Result{OK: false, Error: err.Error()})
	}

	limit := args.MaxResults
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	prompt := fmt.Sprintf("Search the web for: %s\n\nReturn at most %d results as plain text, one per line:\n"+
		"<title> — <url> — <one-sentence snippet>\nNo preamble, no markdown.", query, limit)

	out, err := eng.Generate(ctx, searchSystemPrompt, prompt, llm.SamplingOptions{
		MaxTokens:   800,
		Temperature: 0.2,
		TopP:        0.9,
	})
	if err != nil {
		return tools.EncodeResult(tools.Result{OK: false, Error: "search failed: " + err.Error()})
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return tools.EncodeResult(tools.Result{OK: false, Error: "search returned nothing"})
	}
	return tools.EncodeResult(tools.Result{
		OK:      true,
		Content: out,
		Error:   fmt.Sprintf("query: %s (model: %s)", query, s.searchModel()),
	})
}

// searchSystemPrompt ограничивает ответ рамками поисковой выдачи.
const searchSystemPrompt = "You are a web search backend. Answer with search results only: " +
	"title, url and a one-sentence snippet per line, no commentary."

// searchEngineFor возвращает (создавая при первом обращении) движок
// search-модели.
func (s *session) searchEngineFor() (llm.Engine, error) {
	if s.searchEngine != nil {
		return s.searchEngine, nil
	}
	eng, err := llm.New(llm.Params{
		Provider:      llm.ProviderPollinations,
		RemoteModel:   s.searchModel(),
		RemoteBaseURL: s.cfg.RemoteBaseURL,
		Timeout:       searchTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("search engine: %w", err)
	}
	s.searchEngine = eng
	return eng, nil
}

func (s *session) searchModel() string {
	if m := strings.TrimSpace(s.cfg.SearchModel); m != "" {
		return m
	}
	return config.DefaultSearchModel
}

// promptToolNames возвращает имена инструментов, о которых стоит сказать
// модели в системном промпте. Пусто, если tools не поддерживаются движком:
// тогда промпт остаётся прежним, без упоминаний о несуществующих функциях.
func (s *session) promptToolNames() []string {
	if _, ok := s.toolEngine(); !ok {
		return nil
	}
	specs := s.tools.Specs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

// registerOptionalTools добавляет инструменты, которым нужен доступ к
// сессии: вопрос пользователю и веб-поиск. Вызывается один раз при старте.
func (s *session) registerOptionalTools() {
	s.tools.Register(tools.AskSpec(), func(context.Context, string) (string, error) {
		return "", errors.New("ask must be answered by the user")
	})
	s.tools.Register(tools.WebSearchSpec(s.searchModel()), func(context.Context, string) (string, error) {
		return "", errors.New("websearch must go through the provider search model")
	})
	s.tools.Register(tools.TodoSpec(), tools.NewTodoRunner(s.todos))
}

// runCommandTool исполняет команду, запрошенную моделью через
// run_command. Путь ровно тот же, что и у обычного ответа модели:
// политика безопасности, allowlist, подтверждение, аудит.
func (s *session) runCommandTool(ctx context.Context, call llm.ToolCall, out io.Writer) string {
	var args tools.RunCommandArgs
	if err := call.DecodeArgs(&args); err != nil {
		return tools.EncodeResult(tools.Result{OK: false, Error: "invalid arguments: " + err.Error()})
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return tools.EncodeResult(tools.Result{OK: false, Error: "command is required"})
	}

	// В dry-run инструменты не исполняют команды: модель получает отказ и
	// должна вернуть команду в JSON-ответе.
	if s.cfg.DryRun {
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "dry-run is enabled: commands are not executed, answer with the command in your JSON instead",
		})
	}

	dec := policy.Evaluate(command, prompt.RiskMedium, s.cfg.DangerPatterns, s.cfg.SuspiciousPatterns)
	if dec.Allowed && allowlisted(command, s.cfg.Allowlist) {
		dec.Risk = prompt.RiskLow
	}
	if !dec.Allowed {
		s.audit(command, "tool", dec, executor.Result{})
		return tools.EncodeResult(tools.Result{
			OK:    false,
			Error: "blocked by security policy: " + dec.Reason,
		})
	}

	if dec.Risk != prompt.RiskLow && !s.autoYes {
		// В TUI подтверждениями занимается сама оболочка, а у потока
		// инструментов нет доступа к строке ввода — такие команды
		// возвращаются модели в JSON, чтобы она не исполняла их молча.
		if s.input == nil {
			return tools.EncodeResult(tools.Result{
				OK:    false,
				Error: "confirmation is required, but dmsh is running interactively: return this command in your JSON answer instead",
			})
		}
		ok, err := s.confirmOK(out, "execute tool command?")
		if err != nil {
			return tools.EncodeResult(tools.Result{OK: false, Error: err.Error()})
		}
		if !ok {
			return tools.EncodeResult(tools.Result{OK: false, Error: "user declined to run the command"})
		}
	}

	res := executor.Run(ctx, s.cfg.Shell, command)
	s.addRecentAndHistory(command, "tool")
	s.audit(command, "tool", dec, res)

	content := res.Stdout
	if res.Stderr != "" {
		content += "\n[stderr]\n" + res.Stderr
	}
	if content == "" && res.Err != nil {
		content = res.Err.Error()
	}
	return tools.EncodeResult(tools.Result{
		OK:       res.ExitCode == 0 && res.Err == nil,
		Content:  content,
		ExitCode: res.ExitCode,
	})
}
