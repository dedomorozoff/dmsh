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

func TestValidate_InfersIntentFromFields(t *testing.T) {
	cases := []struct {
		name string
		resp Response
		want Intent
	}{
		{"command", Response{Command: "ls"}, IntentRunCommand},
		{"question", Response{Question: "which dir?"}, IntentAskClarification},
		{"explanation", Response{Explanation: "explains"}, IntentExplain},
	}
	for _, tc := range cases {
		if err := tc.resp.Validate(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.resp.Intent != tc.want {
			t.Fatalf("%s: intent = %q, want %q", tc.name, tc.resp.Intent, tc.want)
		}
	}
}

func TestValidate_EmptyEverythingStillErrors(t *testing.T) {
	r := Response{}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for an entirely empty response")
	}
}

func TestParse_MissingIntentInfersRunCommand(t *testing.T) {
	raw := `{"command":"ls -la","explanation":"list files","risk_level":"low"}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Intent != IntentRunCommand || r.Command != "ls -la" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestParse_MissingIntentInfersClarification(t *testing.T) {
	raw := `{"question":"which directory?"}`
	r, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Intent != IntentAskClarification || r.Question != "which directory?" {
		t.Fatalf("bad parse: %+v", r)
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
