// Package tools описывает инструменты (function calling), которые модель
// может вызвать вместо того, чтобы угадывать команду вслепую.
//
// Реализации read-only инструментов живут здесь же. Команды оболочки сюда
// не попадают намеренно: их исполнение обязано пройти через политику
// безопасности и подтверждение пользователя, поэтому run_command
// регистрируется слоем CLI.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/llm"
)

// Имена встроенных инструментов.
const (
	NameRunCommand = "run_command"
	NameReadFile   = "read_file"
	NameListDir    = "list_dir"
	NameSystemInfo = "system_info"
	NameTodo       = "todo"
	NameAsk        = "ask"
	NameWebSearch  = "websearch"
)

// Ограничения вывода: модель получает усечённый результат, иначе один
// гигантский листинг съест весь контекст.
const (
	MaxOutputBytes = 16 * 1024
	defaultMaxRead = 8 * 1024
	defaultList    = 200
)

// RunCommandArgs — аргументы run_command.
type RunCommandArgs struct {
	Command string `json:"command"`
	Reason  string `json:"reason,omitempty"`
}

// ReadFileArgs — аргументы read_file.
type ReadFileArgs struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// ListDirArgs — аргументы list_dir.
type ListDirArgs struct {
	Path  string `json:"path"`
	Limit int    `json:"limit,omitempty"`
}

// Result — результат вызова инструмента, который уходит модели.
type Result struct {
	OK       bool   `json:"ok"`
	Content  string `json:"content,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

// Runner исполняет один вызов инструмента и возвращает готовый текст для
// модели. Смысл ошибки должен быть в тексте: модель должна видеть, что
// пошло не так, и попробовать иначе.
type Runner func(ctx context.Context, rawArgs string) (string, error)

// Registry — набор инструментов, доступных модели в текущей сессии.
type Registry struct {
	order []string
	items map[string]entry
}

type entry struct {
	spec llm.ToolSpec
	run  Runner
}

// NewRegistry возвращает реестр с read-only инструментами. Команду
// оболочки добавляет вызывающая сторона через Register.
func NewRegistry() *Registry {
	r := &Registry{items: make(map[string]entry, 4)}
	r.Register(ReadFileSpec(), runReadFile)
	r.Register(ListDirSpec(), runListDir)
	r.Register(SystemInfoSpec(), runSystemInfo)
	return r
}

// Register добавляет или заменяет инструмент.
func (r *Registry) Register(spec llm.ToolSpec, run Runner) {
	name := strings.TrimSpace(spec.Name)
	if name == "" || run == nil {
		return
	}
	if spec.Type == "" {
		spec.Type = "function"
	}
	if _, ok := r.items[name]; !ok {
		r.order = append(r.order, name)
	}
	r.items[name] = entry{spec: spec, run: run}
}

// Specs возвращает схемы инструментов в порядке регистрации.
func (r *Registry) Specs() []llm.ToolSpec {
	out := make([]llm.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		if it, ok := r.items[name]; ok {
			out = append(out, it.spec)
		}
	}
	return out
}

// Has сообщает, что инструмент зарегистрирован.
func (r *Registry) Has(name string) bool {
	_, ok := r.items[name]
	return ok
}

// Unregister убирает инструмент: он перестаёт и вызываться, и показываться
// модели в списке схем.
func (r *Registry) Unregister(name string) {
	if _, ok := r.items[name]; !ok {
		return
	}
	delete(r.items, name)
	kept := r.order[:0]
	for _, n := range r.order {
		if n != name {
			kept = append(kept, n)
		}
	}
	r.order = kept
}

// Call выполняет инструмент. Ошибка не возвращается наружу, а упаковывается
// в JSON-результат: падение одного вызова не должно прерывать диалог.
func (r *Registry) Call(ctx context.Context, name, rawArgs string) string {
	it, ok := r.items[name]
	if !ok {
		return EncodeResult(Result{OK: false, Error: fmt.Sprintf("unknown tool %q", name)})
	}
	out, err := it.run(ctx, rawArgs)
	if err != nil {
		return EncodeResult(Result{OK: false, Error: err.Error()})
	}
	return out
}

// EncodeResult упаковывает результат в JSON с усечением вывода.
func EncodeResult(res Result) string {
	res.Content = Truncate(res.Content)
	data, err := json.Marshal(res)
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return string(data)
}

// Truncate обрезает вывод, помечая обрезку, чтобы модель не приняла
// усечённые данные за полные.
func Truncate(s string) string {
	if len(s) <= MaxOutputBytes {
		return s
	}
	return s[:MaxOutputBytes] + "\n...[truncated]"
}

// ReadFileSpec — схема чтения текстового файла.
func ReadFileSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        NameReadFile,
		Description: "Read a UTF-8 text file from disk. Use it to inspect configs, logs and sources instead of guessing their content.",
		Parameters: objectSchema(map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Maximum number of bytes to return (default 8192).",
			},
		}, "path"),
	}
}

// ListDirSpec — схема листинга директории.
func ListDirSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        NameListDir,
		Description: "List directory entries with their size and modification time.",
		Parameters: objectSchema(map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to list. Empty string means the current directory.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of entries to return (default 200).",
			},
		}),
	}
}

// SystemInfoSpec — схема сведений о системе.
func SystemInfoSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        NameSystemInfo,
		Description: "Report OS, architecture, CPU count, shell and working directory of the current session.",
		Parameters:  objectSchema(nil),
	}
}

// RunCommandSpec — схема запуска команды оболочки. Регистрируется CLI,
// потому что выполнение проходит через политику безопасности.
func RunCommandSpec(shell string) llm.ToolSpec {
	desc := "Run a single shell command and return its exit code, stdout and stderr."
	if shell != "" {
		desc = "Run a single shell command (" + shell + ") and return its exit code, stdout and stderr."
	}
	return llm.ToolSpec{
		Name:        NameRunCommand,
		Description: desc + " Prefer it over guessing when you need the real state of the system.",
		Parameters: objectSchema(map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Single-line command to execute.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Short explanation of why this command is needed.",
			},
		}, "command"),
	}
}

// AskArgs — аргументы инструмента ask.
type AskArgs struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
}

// AskSpec — схема уточняющего вопроса пользователю. Регистрируется CLI:
// ответ читается из сессионного ввода, которого у инструментов нет.
func AskSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        NameAsk,
		Description: "Ask the user one focused question mid-task and wait for the answer. Use it only when the request is genuinely ambiguous; prefer a sensible default otherwise.",
		Parameters: objectSchema(map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to ask, in the user's language.",
			},
			"choices": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional short answers to offer the user.",
			},
		}, "question"),
	}
}

// WebSearchArgs — аргументы инструмента websearch.
type WebSearchArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// WebSearchSpec — схема веб-поиска. Результаты берёт провайдер инференса
// через свою search-модель, поэтому отдельных ключей не требуется.
func WebSearchSpec(searchModel string) llm.ToolSpec {
	desc := "Search the web and get a short list of results (title, url, snippet)."
	if searchModel != "" {
		desc += " Backed by the " + searchModel + " search model."
	}
	return llm.ToolSpec{
		Name:        NameWebSearch,
		Description: desc + " Use it for facts you cannot get from the local machine, such as release notes or current versions.",
		Parameters: objectSchema(map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "How many results to ask for (1-10, default 5).",
			},
		}, "query"),
	}
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object"}
	if len(props) > 0 {
		schema["properties"] = props
	} else {
		schema["properties"] = map[string]any{}
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func runReadFile(_ context.Context, rawArgs string) (string, error) {
	var args ReadFileArgs
	if err := json.Unmarshal([]byte(orEmptyObject(rawArgs)), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	limit := args.MaxBytes
	if limit <= 0 || limit > MaxOutputBytes {
		limit = defaultMaxRead
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > limit {
		data = append(data[:limit], []byte("\n...[truncated]")...)
	}
	return EncodeResult(Result{OK: true, Content: string(data)}), nil
}

func runListDir(_ context.Context, rawArgs string) (string, error) {
	var args ListDirArgs
	if err := json.Unmarshal([]byte(orEmptyObject(rawArgs)), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	dir := strings.TrimSpace(args.Path)
	if dir == "" {
		dir = "."
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultList
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var b strings.Builder
	for i, e := range entries {
		if i >= limit {
			fmt.Fprintf(&b, "...[%d more]\n", len(entries)-limit)
			break
		}
		info, err := e.Info()
		switch {
		case err != nil:
			b.WriteString(e.Name() + "\n")
		case e.IsDir():
			b.WriteString(e.Name() + "/\n")
		default:
			fmt.Fprintf(&b, "%s\t%d bytes\t%s\n", e.Name(), info.Size(), info.ModTime().Format("2006-01-02 15:04"))
		}
	}
	return EncodeResult(Result{OK: true, Content: b.String()}), nil
}

func runSystemInfo(_ context.Context, _ string) (string, error) {
	cwd, _ := os.Getwd()
	lines := []string{
		"os: " + runtime.GOOS + "/" + runtime.GOARCH,
		"cpu_cores: " + strconv.Itoa(runtime.NumCPU()),
		"cwd: " + cwd,
	}
	if runtime.GOOS == "windows" {
		lines = append(lines, "shell: powershell")
	} else if shell := os.Getenv("SHELL"); shell != "" {
		lines = append(lines, "shell: "+shell)
	}
	return EncodeResult(Result{OK: true, Content: strings.Join(lines, "\n")}), nil
}

func orEmptyObject(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}
