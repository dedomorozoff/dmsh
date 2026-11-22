package llm

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/diag"
)

// Provider hints must come from diag so the CLI prints one wording per cause.
func TestProviderHintsUseDiagWording(t *testing.T) {
	if got := transportHint(remoteProviderOllama); !strings.Contains(got, diag.HintOllamaNotRunning) {
		t.Fatalf("transportHint = %q, want %q", got, diag.HintOllamaNotRunning)
	}
	if got := transportHint(remoteProviderPollinations); got != "" {
		t.Fatalf("transportHint for Pollinations = %q, want empty", got)
	}

	cases := []struct {
		name string
		code int
		want string
	}{
		{remoteProviderOllama, http.StatusNotFound, diag.HintOllamaModel},
		{remoteProviderPollinations, http.StatusUnauthorized, diag.HintAPIKeyRequired},
		{remoteProviderPollinations, http.StatusForbidden, diag.HintAPIKeyRequired},
		{remoteProviderPollinations, http.StatusPaymentRequired, diag.HintOutOfPollen},
		{remoteProviderPollinations, http.StatusTooManyRequests, diag.HintRateLimited},
		{remoteProviderPollinations, http.StatusNotFound, diag.HintUnknownModel},
	}
	for _, tc := range cases {
		if got := remoteErrorHint(tc.name, tc.code); got != tc.want {
			t.Errorf("remoteErrorHint(%q, %d) = %q, want %q", tc.name, tc.code, got, tc.want)
		}
	}
}
