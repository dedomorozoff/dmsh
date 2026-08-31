package cli

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/prompt"
)

func TestRenderCommandHighlightsFlags(t *testing.T) {
	out := renderCommand("git push --force origin main")
	if !strings.Contains(out, cyan) || !strings.Contains(out, "--force") {
		t.Fatalf("flag not highlighted: %q", out)
	}
	if strings.Contains(out, reset) == false {
		t.Fatalf("missing color reset: %q", out)
	}
	// plain tokens must stay uncolored
	if strings.Count(out, reset) < 1 {
		t.Fatalf("expected at least one reset sequence, got %q", out)
	}
}

func TestRenderCommandNoFlags(t *testing.T) {
	out := renderCommand("ls -la")
	if !strings.Contains(out, "-la") {
		t.Fatalf("flag lost: %q", out)
	}
}

func TestRiskColor(t *testing.T) {
	if riskColor(prompt.RiskHigh) != red {
		t.Errorf("high risk should be red")
	}
	if riskColor(prompt.RiskMedium) != yellow {
		t.Errorf("medium risk should be yellow")
	}
	if riskColor(prompt.RiskLow) != green {
		t.Errorf("low risk should be green")
	}
}
