package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

func TestTextEditHelpers(t *testing.T) {
	s := "one two three"
	pos := len(s)
	if got := deleteWordBack(s, &pos); got != "one two " || pos != 8 {
		t.Fatalf("deleteWordBack = %q/%d", got, pos)
	}
	if got := deleteWordBackPos(0, s); got != 0 {
		t.Fatalf("back pos at 0 = %d", got)
	}
	if got := deleteWordBackPos(len(s), s); got != 8 {
		t.Fatalf("back pos = %d, want 8", got)
	}
	if got := nextWordPos(0, s); got != 4 {
		t.Fatalf("next word pos = %d, want 4", got)
	}
	pos = 4
	if got := deleteWordForward(s, &pos); got != "one three" {
		t.Fatalf("deleteWordForward = %q", got)
	}
	pos = len(s)
	if got := deleteWordForward(s, &pos); got != s {
		t.Fatalf("deleteWordForward at end = %q", got)
	}
	if got := runeSliceLen("привет"); got != 6 {
		t.Fatalf("rune length = %d", got)
	}
}

func TestProgressHelpers(t *testing.T) {
	for _, pct := range []int{-1, 0, 50, 150} {
		if got := progressBar(pct); got == "" {
			t.Fatalf("progressBar(%d) is empty", pct)
		}
	}
	var out bytes.Buffer
	progressFn(&out)(2<<20, 4<<20)
	if !strings.Contains(out.String(), "50%") {
		t.Fatalf("progress output = %q", out.String())
	}
	if !strings.Contains(boolStr(true), "allowed") || !strings.Contains(boolStr(false), "blocked") {
		t.Fatal("boolStr must describe the policy decision")
	}
}

func TestExitErrorHelpers(t *testing.T) {
	inner := errors.New("boom")
	e := &ExitError{Code: 7, Err: inner}
	if !strings.Contains(e.Error(), "exit 7") || !errors.Is(e, inner) {
		t.Fatalf("ExitError = %q, unwrap = %v", e.Error(), e.Unwrap())
	}
	if got := (&ExitError{}).Error(); got != "exit 0" {
		t.Fatalf("bare ExitError = %q", got)
	}
	var nilErr *ExitError
	if nilErr.Error() != "" || nilErr.Unwrap() != nil || nilErr.ExitCode() != 1 {
		t.Fatal("nil ExitError must be harmless")
	}
}

func TestModeSwitcherAndHelp(t *testing.T) {
	var out bytes.Buffer
	cfg := config.Config{Mode: config.ModeAI}
	ms := NewModeSwitcher(&cfg, &out)
	ms.ShowCurrent()
	if !strings.Contains(out.String(), "mode:") {
		t.Fatalf("ShowCurrent output = %q", out.String())
	}
	out.Reset()
	ms.Switch(config.ModeHelp)
	if cfg.Mode != config.ModeHelp || !strings.Contains(out.String(), "help") {
		t.Fatalf("Switch = %q, mode = %q", out.String(), cfg.Mode)
	}
	out.Reset()
	showHelp(&out)
	showKeyBindings(&out)
	if !strings.Contains(out.String(), "/alias") || !strings.Contains(out.String(), "Ctrl+A") {
		t.Fatal("help output is incomplete")
	}
}

func TestLineReaderAndFlags(t *testing.T) {
	r := NewBufioReader(strings.NewReader("first\nsecond\n"))
	line, err := r.ReadLine()
	if err != nil || line != "first" {
		t.Fatalf("first line = %q, %v", line, err)
	}
	if _, err := r.ReadLine(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadLine(); !errors.Is(err, os.ErrClosed) && err == nil {
		t.Fatal("reader at EOF must fail")
	}

	var p config.Provider
	pv := providerValue{&p}
	if err := pv.Set(" OLLAMA "); err != nil || p != config.ProviderOllama || pv.Type() != "provider" {
		t.Fatalf("provider flag = %q, %v", p, err)
	}
	if err := pv.Set("bogus"); err == nil {
		t.Fatal("unknown provider must be rejected")
	}

	var mode netproxy.Mode
	mv := proxyModeValue{&mode}
	if err := mv.Set("off"); err != nil || mode != netproxy.ModeOff || mv.Type() != "proxy-mode" {
		t.Fatalf("proxy flag = %q, %v", mode, err)
	}
	if err := mv.Set("bogus"); err == nil {
		t.Fatal("unknown proxy mode must be rejected")
	}
}

func TestSmallPureHelpers(t *testing.T) {
	if got := splitCSV(" a, b ,, c "); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("splitCSV = %v", got)
	}
	home, err := os.UserHomeDir()
	if err == nil && expandHome("~/dmsh") != filepath.Join(home, "dmsh") {
		t.Fatalf("expandHome = %q", expandHome("~/dmsh"))
	}
	if expandHome("plain") != "plain" {
		t.Fatal("expandHome must leave plain paths alone")
	}

	if !hasExecutableCommand(prompt.Response{Intent: prompt.IntentRunCommand}) {
		t.Fatal("run_command must be executable")
	}
	if hasExecutableCommand(prompt.Response{Intent: prompt.IntentExplain}) {
		t.Fatal("explain without a command must not be executable")
	}
	if !hasExecutableCommand(prompt.Response{Intent: prompt.IntentExplain, Command: "ls"}) {
		t.Fatal("explain with a command must be executable")
	}
	if dec := directDecision(); !dec.Allowed || dec.Risk != prompt.RiskLow {
		t.Fatalf("directDecision = %+v", dec)
	}

	s := &session{}
	s.setSessionName("work")
	if s.sessionName != "work" {
		t.Fatalf("session name = %q", s.sessionName)
	}
}

func TestConfirmWithoutInputReaderRefuses(t *testing.T) {
	s := &session{}
	var out bytes.Buffer
	ok, err := s.confirmOK(&out, "execute?")
	if ok || err != nil {
		t.Fatalf("confirm with no reader = %v, %v", ok, err)
	}
}

func TestConfirmOKAndProxyAddressFlag(t *testing.T) {
	yes := &session{autoYes: true}
	if ok, err := yes.confirmOK(io.Discard, "execute?"); !ok || err != nil {
		t.Fatalf("autoYes confirm = %v, %v", ok, err)
	}

	reader := &session{input: NewBufioReader(strings.NewReader("y\n"))}
	var out bytes.Buffer
	if ok, err := reader.confirmOK(&out, "execute?"); !ok || err != nil {
		t.Fatalf("reader confirm = %v, %v", ok, err)
	}

	cfg := config.Config{}
	pa := proxyAddressValue{&cfg}
	if err := pa.Set("http://user:pass@proxy.local:8080"); err != nil {
		t.Fatal(err)
	}
	if pa.Type() != "proxy-address" || !strings.Contains(pa.String(), "proxy.local") {
		t.Fatalf("proxy flag = %q (%s)", pa.String(), pa.Type())
	}
	if err := pa.Set("://bad"); err == nil {
		t.Fatal("an invalid proxy URL must be rejected")
	}
}

func TestDebugLogger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "dmsh-debug.log")
	l, err := newDebugLogger(true, path, "supersecret")
	if err != nil {
		t.Fatal(err)
	}
	l.printf("key=%s", "supersecret")
	l.event("request", "done")
	l.close()
	l.printf("after close")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "debug logging enabled") || !strings.Contains(text, "request") {
		t.Fatalf("debug log = %q", text)
	}
	if strings.Contains(text, "supersecret") {
		t.Fatalf("secret leaked into debug log: %q", text)
	}

	off, err := newDebugLogger(false, "")
	if err != nil {
		t.Fatal(err)
	}
	off.printf("ignored")
	off.event("ignored")
	off.close()
}

func TestHistoryAuditAndDoctorOutput(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "history.jsonl")
	if err := os.WriteFile(hist, []byte(`{"timestamp":"2026-01-01T00:00:00Z","command":"echo hi","source":"llm"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printHistoryLimit(&out, hist, 1)
	if !strings.Contains(out.String(), "echo hi") {
		t.Fatalf("history output = %q", out.String())
	}
	out.Reset()
	printHistoryLimit(&out, filepath.Join(dir, "missing"), 0)
	if !strings.Contains(out.String(), "No history") {
		t.Fatalf("empty history output = %q", out.String())
	}

	audit := filepath.Join(dir, "audit.jsonl")
	line := `{"timestamp":"2026-01-01T00:00:00Z","command":"ls","source":"llm","risk":"low","allowed":true,"success":true}` + "\n"
	if err := os.WriteFile(audit, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	printAuditLimit(&out, audit, 0)
	if !strings.Contains(out.String(), "Command Audit") || !strings.Contains(out.String(), "allowed") {
		t.Fatalf("audit output = %q", out.String())
	}
	out.Reset()
	printAuditLimit(&out, "", 0)
	if !strings.Contains(out.String(), "No audit entries") {
		t.Fatalf("empty audit output = %q", out.String())
	}

	out.Reset()
	printDoctor(&out, []doctorCheck{
		{Name: "config", Status: doctorOK, Message: "fine"},
		{Name: "model", Status: doctorWarn, Message: "no models", Hint: "download one"},
		{Name: "proxy", Status: doctorFail, Message: "bad proxy", Detail: "port 0"},
	})
	if !strings.Contains(out.String(), "config") || !strings.Contains(out.String(), "download one") {
		t.Fatalf("doctor output = %q", out.String())
	}
}

func TestListNamedSessionsAndSearchPrompt(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	names, err := listNamedSessions()
	if err != nil || len(names) != 0 {
		t.Fatalf("empty sessions = %v, %v", names, err)
	}
	sdir := filepath.Join(dir, "dmsh", "sessions")
	if err := os.MkdirAll(sdir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work.json", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(sdir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names, err = listNamedSessions()
	if err != nil || len(names) != 1 || names[0] != "work" {
		t.Fatalf("sessions = %v, %v", names, err)
	}
	if got := (tuiModel{searchQuery: "abc"}).searchPrompt(); !strings.Contains(got, "abc") {
		t.Fatalf("search prompt = %q", got)
	}
}

func TestApplyRemoteBaseURLValidatesBeforeRebuild(t *testing.T) {
	s := &session{}
	if err := s.applyRemoteBaseURL("ftp://bad"); err == nil {
		t.Fatal("an invalid endpoint must be rejected")
	}
	if s.cfg.RemoteBaseURL != "" {
		t.Fatal("a rejected endpoint must not be written to the session")
	}
}
