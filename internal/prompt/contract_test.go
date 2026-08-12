package prompt

import "testing"

func TestParse_PlainObject(t *testing.T) {
	raw := `{"intent":"run_command","command":"ls -la","explanation":"list files","risk_level":"low","needs_confirmation":false}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Intent != IntentRunCommand || r.Command != "ls -la" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestParse_WithProseAround(t *testing.T) {
	raw := "Sure, here's the JSON:\n```json\n{\"intent\":\"explain\",\"explanation\":\"hello world\"}\n```\nThanks!"
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Intent != IntentExplain || r.Explanation != "hello world" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestParse_BracesInString(t *testing.T) {
	raw := `{"intent":"run_command","command":"echo {hello}","explanation":"echo with braces","risk_level":"low"}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Command != "echo {hello}" {
		t.Fatalf("bad command: %q", r.Command)
	}
}

func TestParse_NoJSON(t *testing.T) {
	if _, err := Parse("just a sentence, no json"); err == nil {
		t.Fatal("expected error for missing JSON")
	}
}

func TestValidate_RunCommand_RequiresCommand(t *testing.T) {
	r := Response{Intent: IntentRunCommand}
	if err := r.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidate_RunCommand_HighRiskAutoConfirm(t *testing.T) {
	r := Response{Intent: IntentRunCommand, Command: "rm file", Risk: RiskHigh}
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !r.NeedsConfirmation {
		t.Fatal("high risk must auto-set NeedsConfirmation")
	}
}

func TestValidate_AskClarification_RequiresQuestion(t *testing.T) {
	r := Response{Intent: IntentAskClarification}
	if err := r.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidate_UnknownIntent(t *testing.T) {
	r := Response{Intent: "weird"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown intent")
	}
}

func TestParse_FallbackParser(t *testing.T) {
	// Case 1: Trailing comma (invalid JSON, but captured by fallback)
	rawTrailing := `{
		"intent": "run_command",
		"command": "ls -l",
		"explanation": "list files",
		"risk_level": "low",
	}`
	r, err := Parse(rawTrailing)
	if err != nil {
		t.Fatalf("expected fallback parsing to succeed, got error: %v", err)
	}
	if r.Intent != IntentRunCommand || r.Command != "ls -l" || r.Explanation != "list files" {
		t.Fatalf("unexpected parsed values: %+v", r)
	}

	// Case 2: Truncated JSON without closing brackets
	rawTruncated := `{
		"intent": "run_command",
		"command": "echo \"hello world\"",
		"explanation": "print greeting"
	`
	r2, err := Parse(rawTruncated)
	if err != nil {
		t.Fatalf("expected fallback parsing to succeed for truncated JSON, got error: %v", err)
	}
	if r2.Intent != IntentRunCommand || r2.Command != `echo "hello world"` || r2.Explanation != "print greeting" {
		t.Fatalf("unexpected parsed values: %+v", r2)
	}
}
