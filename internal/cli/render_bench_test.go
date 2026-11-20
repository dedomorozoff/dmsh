package cli

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
)

func newBenchModel(lines int) tuiModel {
	cfg := config.Default()
	rf := &rootFlags{cfg: cfg}
	m := NewTuiModel(rf, &session{cfg: cfg, engine: &captureEngine{}}).(tuiModel)
	m.width = 120
	m.height = 40

	var b strings.Builder
	for i := 0; i < lines; i++ {
		if i%7 == 0 {
			b.WriteString("$ go test ./internal/cli -count=1 -timeout 10m && echo 'a fairly long line that should wrap at the terminal width'\n")
		} else {
			b.WriteString("  ok  github.com/dedomorozoff/dmsh/internal/cli\n")
		}
	}
	m.content = b.String()
	m.scrollbackLines = lines
	return m
}

// BenchmarkRender4000 меряет кадр при неизменном транскрипте: так работает
// простой интерфейс и любой кадр, где модель не печатает.
func BenchmarkRender4000(b *testing.B) {
	m := newBenchModel(4000)
	_ = m.render() // прогрев кеша
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.render()
	}
}

// BenchmarkRenderStreaming меряет кадр во время стрима: каждый шаг дописывает
// токен в конец и перерисовывает кадр.
func BenchmarkRenderStreaming(b *testing.B) {
	m := newBenchModel(4000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.appendScrollback("token ")
		_ = m.render()
	}
}
