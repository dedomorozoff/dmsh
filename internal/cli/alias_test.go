package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func TestExpandAlias(t *testing.T) {
	cfg := config.Config{Aliases: map[string]string{
		"gs":    "show all txt files",
		"Empty": "   ",
	}}
	cases := []struct {
		in, want string
	}{
		{"gs", "show all txt files"},
		{"  GS  ", "show all txt files"},
		{"Empty", "Empty"},
		{"unknown", "unknown"},
		{"/alias", "/alias"},
		{"!ls", "!ls"},
	}
	for _, tc := range cases {
		if got := expandAlias(cfg, tc.in); got != tc.want {
			t.Errorf("expandAlias(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The model must see the expanded request, not the alias name.
func TestAskStreamUsesExpandedAlias(t *testing.T) {
	eng := &captureEngine{tokens: []string{`{"intent":"explain","explanation":"ok"}`}}
	s := newToolSession(t, eng)
	s.cfg.Aliases = map[string]string{"gs": "show all txt files"}

	var out bytes.Buffer
	if _, _, err := s.askStream(context.Background(), "ask", "gs", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eng.user, "show all txt files") {
		t.Fatalf("the prompt must contain the expanded alias:\n%s", eng.user)
	}
}
