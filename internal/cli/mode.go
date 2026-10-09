package cli

import (
	"fmt"
	"io"

	"github.com/dedomorozoff/dmsh/internal/config"
)

// modeOrder — порядок циклического переключения по Shift+Tab. Терминал
// идёт последним: это состояние, из которого выходят через Enter, и
// ставить его в середину цикла неудобно.
var modeOrder = []config.Mode{config.ModeAI, config.ModeHelp, config.ModeShell}

// modeLabel — человекочитаемое имя режима для UI.
func modeLabel(m config.Mode) string {
	switch m {
	case config.ModeHelp:
		return "help"
	case config.ModeShell:
		return "terminal"
	case config.ModeAI:
		return "ai"
	default:
		return string(m)
	}
}

// nextMode возвращает режим после текущего в цикле modeOrder.
func nextMode(m config.Mode) config.Mode {
	for i, mode := range modeOrder {
		if mode == m {
			return modeOrder[(i+1)%len(modeOrder)]
		}
	}
	// Неизвестный режим (например, из старого конфига): начинаем цикл заново.
	return modeOrder[0]
}

// ModeSwitcher управляет переключением режимов в REPL.
type ModeSwitcher struct {
	cfg *config.Config
	out io.Writer
}

// NewModeSwitcher создаёт новый переключатель режимов.
func NewModeSwitcher(cfg *config.Config, out io.Writer) *ModeSwitcher {
	return &ModeSwitcher{cfg: cfg, out: out}
}

// Switch переключает режим и выводит сообщение.
func (m *ModeSwitcher) Switch(newMode config.Mode) {
	m.cfg.Mode = newMode
	fmt.Fprintf(m.out, "%sMode changed to: %s%s\n", green, modeLabel(newMode), reset)
}

// ShowCurrent показывает текущий режим.
func (m *ModeSwitcher) ShowCurrent() {
	fmt.Fprintf(m.out, "%sCurrent mode: %s%s\n", bold, modeLabel(m.cfg.Mode), reset)
}

// ParseModeCommand parses the /mode command and returns the new mode or empty string.
func ParseModeCommand(line string) config.Mode {
	if line == "/mode" {
		return ""
	}
	switch line {
	case "/mode ai", "/mode agent":
		return config.ModeAI
	case "/mode help", "/mode explain":
		return config.ModeHelp
	case "/mode shell", "/mode terminal":
		return config.ModeShell
	}
	return ""
}

// IsModeCommand checks if the command is a mode command.
func IsModeCommand(line string) bool {
	switch line {
	case "/mode", "/mode ai", "/mode agent", "/mode help", "/mode explain",
		"/mode shell", "/mode terminal":
		return true
	}
	return false
}
