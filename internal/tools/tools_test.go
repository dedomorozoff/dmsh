package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/llm"
)

func TestNewRegistrySpecs(t *testing.T) {
	r := NewRegistry()
	specs := r.Specs()
	if len(specs) != 3 {
		t.Fatalf("specs = %d, want 3 read-only tools", len(specs))
	}
	want := map[string]bool{
		NameReadFile:   false,
		NameListDir:    false,
		NameSystemInfo: false,
	}
	for _, spec := range specs {
		if spec.Name == "" || spec.Description == "" {
			t.Fatalf("incomplete spec: %+v", spec)
		}
		if _, ok := want[spec.Name]; !ok {
			t.Fatalf("unexpected tool %q", spec.Name)
		}
		want[spec.Name] = true
		if spec.Parameters == nil {
			t.Fatalf("tool %q has no JSON schema", spec.Name)
		}
		if spec.Parameters["type"] != "object" {
			t.Fatalf("tool %q schema must be an object schema: %v", spec.Name, spec.Parameters)
		}
		// Схема должна быть сериализуемой — она уходит в API как есть.
		if _, err := json.Marshal(spec); err != nil {
			t.Fatalf("tool %q is not serializable: %v", spec.Name, err)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("tool %q missing from registry", name)
		}
	}
}

func TestRunCommandSpecRequiresCommand(t *testing.T) {
	spec := RunCommandSpec("powershell")
	if spec.Name != NameRunCommand {
		t.Fatalf("name = %q", spec.Name)
	}
	if !strings.Contains(spec.Description, "powershell") {
		t.Fatalf("shell should be mentioned in description: %q", spec.Description)
	}
	required, _ := spec.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "command" {
		t.Fatalf("required = %v, want [command]", spec.Parameters["required"])
	}
}

func TestRegisterKeepsOrderAndReplacesRunner(t *testing.T) {
	r := NewRegistry()
	r.Register(llm.ToolSpec{Name: NameRunCommand}, func(context.Context, string) (string, error) {
		return `{"ok":true}`, nil
	})
	if !r.Has(NameRunCommand) {
		t.Fatal("run_command should be registered")
	}
	specs := r.Specs()
	if specs[len(specs)-1].Name != NameRunCommand {
		t.Fatalf("registration order broken: %v", names(specs))
	}
	if got := r.Call(context.Background(), NameRunCommand, "{}"); got != `{"ok":true}` {
		t.Fatalf("call = %q", got)
	}
}

func TestRegisterIgnoresInvalid(t *testing.T) {
	r := NewRegistry()
	before := len(r.Specs())
	r.Register(llm.ToolSpec{Name: ""}, func(context.Context, string) (string, error) { return "", nil })
	r.Register(llm.ToolSpec{Name: "x"}, nil)
	if len(r.Specs()) != before {
		t.Fatalf("registry changed: %v", names(r.Specs()))
	}
}

func TestUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(llm.ToolSpec{Name: NameAsk}, func(context.Context, string) (string, error) {
		return `{"ok":true}`, nil
	})
	if !r.Has(NameAsk) {
		t.Fatal("ask must be registered")
	}
	r.Unregister(NameAsk)
	if r.Has(NameAsk) {
		t.Fatal("ask must be gone")
	}
	if len(r.Specs()) != 3 {
		t.Fatalf("specs after unregister: %v", names(r.Specs()))
	}
	if res := decodeResult(t, r.Call(context.Background(), NameAsk, "{}")); res.OK {
		t.Fatal("unregistered tool must not be callable")
	}
	// Повторный вызов не должен ломать порядок остальных инструментов.
	r.Unregister(NameAsk)
	if got := names(r.Specs()); len(got) != 3 || got[0] != NameReadFile {
		t.Fatalf("specs = %v", got)
	}
}

func TestUnregisterKeepsOrderAfterRemoval(t *testing.T) {
	r := NewRegistry()
	r.Register(llm.ToolSpec{Name: NameAsk}, func(context.Context, string) (string, error) { return "", nil })
	r.Register(llm.ToolSpec{Name: NameTodo}, func(context.Context, string) (string, error) { return "", nil })
	r.Unregister(NameAsk)
	got := names(r.Specs())
	want := []string{NameReadFile, NameListDir, NameSystemInfo, NameTodo}
	if len(got) != len(want) {
		t.Fatalf("specs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("specs = %v, want %v", got, want)
		}
	}
}

func TestCallUnknownTool(t *testing.T) {
	out := NewRegistry().Call(context.Background(), "rm_rf", "{}")
	var res Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result must be JSON: %v (%s)", err, out)
	}
	if res.OK || !strings.Contains(res.Error, "unknown tool") {
		t.Fatalf("result = %+v", res)
	}
}

func TestCallReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello file"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	res := decodeResult(t, r.Call(context.Background(), NameReadFile, `{"path":"`+filepath.ToSlash(path)+`"}`))
	if !res.OK || !strings.Contains(res.Content, "hello file") {
		t.Fatalf("result = %+v", res)
	}
}

func TestCallReadFileErrors(t *testing.T) {
	r := NewRegistry()
	if res := decodeResult(t, r.Call(context.Background(), NameReadFile, `{}`)); res.OK || !strings.Contains(res.Error, "path is required") {
		t.Fatalf("missing path: %+v", res)
	}
	if res := decodeResult(t, r.Call(context.Background(), NameReadFile, `{"path":"/definitely/missing/file"}`)); res.OK {
		t.Fatalf("missing file must fail: %+v", res)
	}
	if res := decodeResult(t, r.Call(context.Background(), NameReadFile, `not-json`)); res.OK {
		t.Fatalf("broken arguments must fail: %+v", res)
	}
}

func TestCallReadFileTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	big := strings.Repeat("x", defaultMaxRead+100)
	if err := os.WriteFile(path, []byte(big), 0600); err != nil {
		t.Fatal(err)
	}
	res := decodeResult(t, NewRegistry().Call(context.Background(), NameReadFile, `{"path":"`+filepath.ToSlash(path)+`"}`))
	if !res.OK {
		t.Fatalf("result = %+v", res)
	}
	if !strings.HasSuffix(res.Content, "...[truncated]") {
		t.Fatalf("long content must be marked as truncated")
	}
	if strings.Contains(res.Content, strings.Repeat("x", defaultMaxRead+1)) {
		t.Fatal("content was not truncated")
	}
}

func TestCallListDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0600); err != nil {
		t.Fatal(err)
	}
	res := decodeResult(t, NewRegistry().Call(context.Background(), NameListDir, `{"path":"`+filepath.ToSlash(dir)+`"}`))
	if !res.OK {
		t.Fatalf("result = %+v", res)
	}
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.HasPrefix(lines[0], "b.txt") {
		t.Fatalf("entries must be sorted: %v", lines)
	}
	if strings.TrimSpace(lines[1]) != "sub/" {
		t.Fatalf("directories must be marked: %q", lines[1])
	}
}

func TestCallListDirLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	res := decodeResult(t, NewRegistry().Call(context.Background(), NameListDir, `{"path":"`+filepath.ToSlash(dir)+`","limit":1}`))
	if !strings.Contains(res.Content, "[2 more]") {
		t.Fatalf("limit must be reported: %q", res.Content)
	}
}

func TestCallSystemInfo(t *testing.T) {
	res := decodeResult(t, NewRegistry().Call(context.Background(), NameSystemInfo, ""))
	if !res.OK || !strings.Contains(res.Content, "os:") || !strings.Contains(res.Content, "cwd:") {
		t.Fatalf("result = %+v", res)
	}
}

func TestEncodeResultTruncates(t *testing.T) {
	out := EncodeResult(Result{OK: true, Content: strings.Repeat("y", MaxOutputBytes+50)})
	if !strings.Contains(out, "...[truncated]") {
		t.Fatal("long output must be truncated")
	}
	if len(out) > MaxOutputBytes+512 {
		t.Fatalf("output is too long: %d bytes", len(out))
	}
}

func TestTruncateKeepsShortText(t *testing.T) {
	if got := Truncate("short"); got != "short" {
		t.Fatalf("Truncate = %q", got)
	}
}

func decodeResult(t *testing.T, raw string) Result {
	t.Helper()
	var res Result
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("result must be JSON: %v (%s)", err, raw)
	}
	return res
}

func names(specs []llm.ToolSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}
