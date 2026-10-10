package cli

import (
	"fmt"
	"io"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// modeOrder — порядок циклического переключения по Shift+Tab. Терминал
// идёт последним: это состояние, из которого выходят тем же Shift+Tab, и
// ставить его в середину цикла неудобно.
var modeOrder = []config.Mode{config.ModeAI, config.ModeHelp, config.ModeShell}

// modeDesc — короткое описание режима: по нему список читается, не
// перебирая /help.
var modeDesc = map[config.Mode]string{
	config.ModeAI:    "the model runs commands automatically",
	config.ModeHelp:  "commands with explanations, nothing is executed",
	config.ModeShell: "an interactive shell inside this window",
}

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

// openModeMenu открывает список режимов. Раньше /mode без аргументов просто
// печатал текущий режим одной строкой: выбрать другой было негде.
func (m tuiModel) openModeMenu() tuiModel {
	m.state = tuiModeMenu
	m.supIdx = modeMenuIndex(m.rf.cfg.Mode)
	return m
}

// modeMenuIndex находит строку текущего режима в списке.
func modeMenuIndex(mode config.Mode) int {
	for i, m := range modeOrder {
		if m == mode {
			return i
		}
	}
	return 0
}

// modeMenuRows собирает содержимое окна выбора режима.
func (m tuiModel) modeMenuRows() []string {
	rows := make([]string, 0, len(modeOrder)+1)
	for i, mode := range modeOrder {
		marker, style := "  ", colorGray
		if i == m.supIdx {
			marker, style = "> ", colorYellow+colorBold
		}
		current := ""
		if mode == m.rf.cfg.Mode {
			current = "  (current)"
		}
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%-9s%s%s %s— %s%s",
			style, marker, modeLabel(mode), current, colorReset, colorGray, modeDesc[mode], colorReset), m.width))
	}
	rows = append(rows, fitWidth(fmt.Sprintf("%s↑/↓ select · Enter apply · Esc close · Shift+Tab cycles%s",
		colorGray, colorReset), m.width))
	return rows
}

// modeMenuModal описывает окно выбора режима для рендера.
func (m tuiModel) modeMenuModal() modalBox {
	return modalBox{title: "mode", rows: m.modeMenuRows(), cursorX: 0, cursorY: m.supIdx}
}

// handleModeMenuKey обрабатывает клавиши в списке режимов. Выбор применяется
// по Enter: смена режима меняет то, куда уходит ввод, и делать это одним
// нажатием стрелки было бы слишком легко.
func (m tuiModel) handleModeMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape || msg.Keystroke() == "ctrl+c":
		return m.closeModeMenu(), nil
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		m.supIdx = (m.supIdx - 1 + len(modeOrder)) % len(modeOrder)
		return m, nil
	case msg.Code == tea.KeyDown || msg.Code == tea.KeyTab ||
		msg.Keystroke() == "ctrl+n" || msg.Keystroke() == "ctrl+i":
		m.supIdx = (m.supIdx + 1) % len(modeOrder)
		return m, nil
	case msg.Code == tea.KeyEnter:
		selected := modeOrder[m.supIdx]
		mm, cmd := m.closeModeMenu().switchMode(selected)
		return mm, cmd
	}
	return m, nil
}

// closeModeMenu возвращает список, не меняя режим.
func (m tuiModel) closeModeMenu() tuiModel {
	m.state = tuiIdle
	m.input = ""
	m.cursorPos = 0
	return m
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

// ShowCurrent показывает режимы списком с отметкой на текущем: одна строка
// «Current mode: ai» не объясняет, какие режимы вообще есть.
func (m *ModeSwitcher) ShowCurrent() {
	fmt.Fprintf(m.out, "\n%s%s=== mode: %s ===%s\n", bold, cyan, modeLabel(m.cfg.Mode), reset)
	for _, mode := range modeOrder {
		marker := "  "
		if mode == m.cfg.Mode {
			marker = "> "
		}
		fmt.Fprintf(m.out, "  %s%s%-9s%s %s— %s%s\n", yellow, marker, modeLabel(mode), reset, gray, modeDesc[mode], reset)
	}
	fmt.Fprintf(m.out, "\n  switch: %sShift+Tab%s (cycle), %sCtrl+P%s (palette) or %s/mode <name>%s\n\n",
		yellow, reset, yellow, reset, yellow, reset)
}
