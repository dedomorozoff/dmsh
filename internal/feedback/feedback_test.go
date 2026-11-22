package feedback

import (
	"regexp"
	"strings"
	"testing"
)

func TestAnalyzeSuccess(t *testing.T) {
	tests := []struct {
		name    string
		command string
		stdout  string
		stderr  string
		code    int
		wantOK  bool
		wantSub string
	}{
		{name: "mkdir success", command: "mkdir test", stdout: "    Directory: C:\\tmp\n", code: 0, wantOK: true, wantSub: "Done"},
		{name: "remove success", command: "rm file.txt", stdout: "", code: 0, wantOK: true, wantSub: "successfully"},
		{name: "copy success", command: "cp a b", stdout: "", code: 0, wantOK: true, wantSub: "successfully"},
		{name: "move success", command: "mv a b", stdout: "", code: 0, wantOK: true, wantSub: "successfully"},
		{name: "empty dir listing", command: "ls", stdout: "", code: 0, wantOK: true, wantSub: "empty"},
		{name: "dir listing with content", command: "dir", stdout: "file1\nfile2\n", code: 0, wantOK: true, wantSub: ""},
		{name: "generic success", command: "echo hi", stdout: "hi", code: 0, wantOK: true, wantSub: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Analyze(tt.command, tt.stdout, tt.stderr, tt.code)
			if r.Success != tt.wantOK {
				t.Fatalf("Success=%v, want %v", r.Success, tt.wantOK)
			}
			if tt.wantSub == "" {
				if r.Hint != "" {
					t.Fatalf("Hint=%q, want empty", r.Hint)
				}
				return
			}
			if !strings.Contains(r.Hint, tt.wantSub) {
				t.Fatalf("Hint=%q, want substring %q", r.Hint, tt.wantSub)
			}
		})
	}
}

func TestAnalyzeError(t *testing.T) {
	tests := []struct {
		name    string
		command string
		stderr  string
		code    int
		wantSub string
	}{
		{name: "already exists", command: "mkdir x", stderr: "already exists", code: 1, wantSub: "already exists"},
		{name: "not found", command: "cat x", stderr: "cannot find path", code: 1, wantSub: "not found"},
		{name: "access denied", command: "cat /root/x", stderr: "Permission denied", code: 1, wantSub: "Permission denied"},
		{name: "file in use", command: "rm x", stderr: "being used by another process", code: 1, wantSub: "another process"},
		{name: "disk full", command: "cp a b", stderr: "disk full", code: 1, wantSub: "disk space"},
		{name: "syntax error", command: "bad", stderr: "syntax error near unexpected token", code: 2, wantSub: "syntax error"},
		{name: "command not recognized", command: "foo", stderr: "not recognized as an internal command", code: 127, wantSub: "Command not found"},
		{name: "timeout", command: "curl", stderr: "connection timed out", code: 1, wantSub: "timed out"},
		{name: "network", command: "curl", stderr: "connection refused", code: 1, wantSub: "Network"},
		{name: "powershell new item error", command: "New-Item x", stderr: "FullyQualifiedErrorId : NewItemIOError", code: 1, wantSub: "create"},
		{name: "powershell path not found", command: "cd x", stderr: "FullyQualifiedErrorId : PathNotFound", code: 1, wantSub: "Path not found"},
		{name: "generic non-zero", command: "x", stderr: "boom", code: 42, wantSub: "42"},
		{name: "non-zero without stderr", command: "x", stderr: "", code: 3, wantSub: "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Analyze(tt.command, "", tt.stderr, tt.code)
			if r.Success {
				t.Fatal("expected failure")
			}
			if r.ExitCode != tt.code {
				t.Fatalf("ExitCode=%d, want %d", r.ExitCode, tt.code)
			}
			if tt.wantSub == "" {
				if r.Hint != "" {
					t.Fatalf("Hint=%q, want empty", r.Hint)
				}
				return
			}
			if !strings.Contains(strings.ToLower(r.Hint), strings.ToLower(tt.wantSub)) {
				t.Fatalf("Hint=%q, want substring %q", r.Hint, tt.wantSub)
			}
		})
	}
}

// The interface is English; detection may read Russian stderr, but hints
// must never be Russian. This guards the language policy from drifting back.
func TestHintsAreEnglish(t *testing.T) {
	cyrillic := regexp.MustCompile(`[\p{Cyrillic}]`)
	cases := []Result{
		Analyze("mkdir x", "", "already exists", 1),
		Analyze("cat x", "", "не найден", 1),
		Analyze("rm x", "", "permission denied", 1),
		Analyze("mkdir x", "Directory: C:\\tmp\n", "", 0),
		Analyze("ls", "", "", 0),
	}
	for _, r := range cases {
		if cyrillic.MatchString(r.Hint) {
			t.Fatalf("hint must be English, got %q", r.Hint)
		}
	}
}

func TestResultFormat(t *testing.T) {
	r := Result{Hint: "подсказка"}
	if got := r.Format(); got != "подсказка" {
		t.Fatalf("Format()=%q, want %q", got, "подсказка")
	}
}
