package cli

import (
	"fmt"
	"io"

	"github.com/dedomorozoff/dmsh/internal/config"
)

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
	fmt.Fprintf(m.out, "%sMode changed to: %s%s\n", green, newMode, reset)
}

// ShowCurrent показывает текущий режим.
func (m *ModeSwitcher) ShowCurrent() {
	fmt.Fprintf(m.out, "%sCurrent mode: %s%s\n", bold, m.cfg.Mode, reset)
}

// ParseModeCommand parses the /mode command and returns the new mode or empty string.
func ParseModeCommand(line string) config.Mode {
	if line == "/mode" {
		return ""
	}
	switch line {
	case "/mode ai", "/mode 1", "/1":
		return config.ModeAI
	case "/mode help", "/mode 2", "/2":
		return config.ModeHelp
	case "/mode shell", "/mode 3", "/3":
		return config.ModeShell
	}
	return ""
}

// IsModeCommand checks if the command is a mode command.
func IsModeCommand(line string) bool {
	switch line {
	case "/mode", "/mode ai", "/mode help", "/mode shell", "/mode 1", "/mode 2", "/mode 3", "/1", "/2", "/3":
		return true
	}
	return false
}
