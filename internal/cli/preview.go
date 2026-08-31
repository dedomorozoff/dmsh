package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// previewAndConfirm показывает команду с подсветкой опций и уровнем риска,
// затем просит явное подтверждение перед выполнением.
func previewAndConfirm(out io.Writer, s *session, cmd string, risk prompt.Risk) (bool, error) {
	fmt.Fprintf(out, "\n%s%s=== Command preview ===%s\n", bold, cyan, reset)
	fmt.Fprintf(out, "%sRisk:%s %s%s%s\n", bold, reset, riskColor(risk), risk, reset)
	fmt.Fprintf(out, "%s$ %s%s%s\n", cyan, reset, renderCommand(cmd), reset)
	return s.confirmOK(out, "execute?")
}

func riskColor(r prompt.Risk) string {
	switch r {
	case prompt.RiskHigh:
		return red
	case prompt.RiskMedium:
		return yellow
	default:
		return green
	}
}

// renderCommand подсвечивает флаги и опции (токены, начинающиеся с '-' или '/').
func renderCommand(cmd string) string {
	var b strings.Builder
	fields := strings.Fields(cmd)
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		if len(f) > 1 && (f[0] == '-' || f[0] == '/') {
			b.WriteString(cyan)
			b.WriteString(f)
			b.WriteString(reset)
		} else {
			b.WriteString(f)
		}
	}
	return b.String()
}
