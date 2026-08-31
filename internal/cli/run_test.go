package cli

import "testing"

func TestAllowlisted(t *testing.T) {
	list := []string{"git status", "ls"}
	cases := []struct {
		cmd  string
		want bool
	}{
		{"git status", true},
		{"git status -s", true},
		{"git statusremote", false},
		{"ls -la", true},
		{"ls", true},
		{"rm -rf /", false},
		{"", false},
	}
	for _, c := range cases {
		if got := allowlisted(c.cmd, list); got != c.want {
			t.Errorf("allowlisted(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestAllowlistedEmptyList(t *testing.T) {
	if allowlisted("ls", nil) {
		t.Fatal("nil allowlist should allow nothing")
	}
}
