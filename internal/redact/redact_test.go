package redact

import "testing"

func TestURLCredentials(t *testing.T) {
	got := Text("connect via https://user:pass@example.com/path")
	want := "connect via https://[REDACTED]@example.com/path"
	if got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
	if got := URL("http://alice:secret@proxy.local:8080"); got != "http://[REDACTED]@proxy.local:8080" {
		t.Fatalf("URL() = %q", got)
	}
}

func TestBearerToken(t *testing.T) {
	got := Text("curl -H 'Authorization: Bearer sk-secret-token' https://api.test")
	if got != "curl -H 'Authorization: Bearer [REDACTED]' https://api.test" {
		t.Fatalf("Text() = %q", got)
	}
}

func TestInlineAssignment(t *testing.T) {
	got := Text("POLLINATIONS_API_KEY=sk-abc123 dmsh ask hi")
	if got != "POLLINATIONS_API_KEY=[REDACTED] dmsh ask hi" {
		t.Fatalf("Text() = %q", got)
	}
}

func TestLiteralSecrets(t *testing.T) {
	got := Text("use sk-topsecret now", "sk-topsecret")
	if got != "use [REDACTED] now" {
		t.Fatalf("Text() = %q", got)
	}
}

func TestSecretsIgnoresShortAndEmpty(t *testing.T) {
	if got := Secrets("ab cd", "", "a", "  "); got != "ab cd" {
		t.Fatalf("Secrets() = %q", got)
	}
}
