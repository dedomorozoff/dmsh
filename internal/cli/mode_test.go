package cli

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dedomorozoff/dmsh/internal/config"
)

// /mode без аргументов открывает список режимов: одна строка «текущий
// режим» ничего не позволяла выбрать.
func TestSlashModeOpensMenu(t *testing.T) {
	m := newTestTui()
	mm, _ := m.handleSlash("/mode")
	got := mm.(tuiModel)
	if got.state != tuiModeMenu {
		t.Fatalf("state = %d, want tuiModeMenu", got.state)
	}
	if got.supIdx != modeMenuIndex(config.ModeAI) {
		t.Fatalf("selection = %d, want the current mode", got.supIdx)
	}
	rows := strings.Join(got.modeMenuRows(), "\n")
	for _, want := range []string{"ai", "help", "terminal", "(current)"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("mode menu should list %q:\n%s", want, rows)
		}
	}
}

func TestModeMenuEnterAppliesMode(t *testing.T) {
	m := newTestTui()
	mm, _ := m.handleSlash("/mode")
	m = mm.(tuiModel)
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown}) // -> help
	mm, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := mm.(tuiModel)
	if got.state != tuiIdle {
		t.Fatalf("state = %d after Enter, want tuiIdle", got.state)
	}
	if got.rf.cfg.Mode != config.ModeHelp {
		t.Fatalf("mode = %q, want help", got.rf.cfg.Mode)
	}
	if got.s.cfg.Mode != config.ModeHelp {
		t.Fatalf("session mode = %q, want help", got.s.cfg.Mode)
	}
	if got.modeLabel != "help" {
		t.Fatalf("modeLabel = %q, want help", got.modeLabel)
	}
}

func TestModeMenuEscapeKeepsMode(t *testing.T) {
	m := newTestTui()
	mm, _ := m.handleSlash("/mode")
	m = mm.(tuiModel)
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != tuiIdle {
		t.Fatalf("state = %d after Esc, want tuiIdle", m.state)
	}
	if m.rf.cfg.Mode != config.ModeAI {
		t.Fatalf("Esc must not change the mode, got %q", m.rf.cfg.Mode)
	}
}

// Меню режимов рисуется как модальное окно: рамка не должна разъезжаться.
func TestModeMenuIsAModal(t *testing.T) {
	m := newTestTui()
	m.width, m.height = 60, 24
	m = m.openModeMenu()
	box, ok := m.modal()
	if !ok {
		t.Fatal("the mode menu must be a modal window")
	}
	lines, _, _ := frameBoxLines(box, m.width)
	want := displayWidth(lines[0])
	for i, row := range lines {
		if got := displayWidth(row); got != want {
			t.Fatalf("line %d is %d wide, want %d", i, got, want)
		}
	}
	if want > m.width {
		t.Fatalf("box is %d wide, want <= %d", want, m.width)
	}
}

// Статусная строка обязана показывать, пойдёт ли запрос через прокси:
// молчаливое «прокси есть» обнаруживается уже по отказу соединения.
func TestStatuslineShowsProxyState(t *testing.T) {
	m := newTestTui()
	m.s.cfg.ProxyMode = ""
	m.s.cfg.ProxyHost = ""
	if got := m.proxyLabel(); got != "proxy:off" {
		t.Fatalf("proxyLabel = %q, want proxy:off", got)
	}
	m.s.cfg.ProxyMode = "custom"
	m.s.cfg.ProxyHost = "proxy.local"
	if got := m.proxyLabel(); got != "proxy:on" {
		t.Fatalf("proxyLabel = %q, want proxy:on", got)
	}
	line := ansi.Strip(m.statusline())
	if !strings.Contains(line, "proxy:on") {
		t.Fatalf("status line should show the proxy state: %s", line)
	}
}
