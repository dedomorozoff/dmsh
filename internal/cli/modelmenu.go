package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/model"
)

// modelMenuItem — одна строка меню моделей (Ctrl+O / Ctrl+P).
type modelMenuItem struct {
	name      string
	info      *model.ModelInfo // задан для рекомендуемых, ещё не скачанных
	installed bool
	isCurrent bool
	sizeBytes int64
}

type modelProgMsg struct {
	pct int // -1 — неизвестный размер
}

type modelDoneMsg struct {
	path       string
	downloaded bool
	loaded     bool
	err        error
}

// waitForModel прокачивает прогресс скачивания/загрузки до завершения.
func waitForModel(ch <-chan modelProgMsg, done <-chan modelDoneMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case p := <-ch:
			return p
		case d := <-done:
			return d
		}
	}
}

func (m tuiModel) openModelMenu() tuiModel {
	m.state = tuiModelMenu
	m.modelItems = m.buildModelMenu()
	m.modelIdx = 0
	for i, it := range m.modelItems {
		if it.isCurrent {
			m.modelIdx = i
		}
	}
	return m
}

// buildModelMenu собирает список: установленные .gguf из папки моделей, затем
// рекомендуемые, которые ещё не скачаны.
func (m tuiModel) buildModelMenu() []modelMenuItem {
	d := model.New("")
	items := make([]modelMenuItem, 0, len(model.RecommendedModels)+4)
	seen := make(map[string]bool, 8)

	if installed, err := d.ListAllModels(); err == nil {
		for _, inf := range installed {
			var size int64 = -1
			if fi, err := os.Stat(d.ModelPath(inf.Name)); err == nil {
				size = fi.Size()
			}
			items = append(items, modelMenuItem{name: inf.Name, installed: true, sizeBytes: size})
			seen[inf.Name] = true
		}
	}

	for i := range model.RecommendedModels {
		rec := model.RecommendedModels[i]
		if seen[rec.Name] {
			continue
		}
		items = append(items, modelMenuItem{
			name:      rec.Name,
			info:      &model.RecommendedModels[i],
			sizeBytes: int64(rec.SizeMB) * 1024 * 1024,
		})
	}

	cur := fmt.Sprint(m.s.cfg.ModelPath)
	if cur == "" {
		cur = m.rf.cfg.ModelPath
	}
	cur = filepath.Base(cur)
	for i := range items {
		items[i].isCurrent = items[i].name == cur
	}
	return items
}

// menuRows — строки, показываемые вместо строки ввода, когда открыто меню.
func (m tuiModel) menuRows() []string {
	var rows []string
	rows = append(rows, hardWrap(fmt.Sprintf("%s=== model menu%s%s (ctrl+o/ctrl+p) ===%s",
		colorBold+colorCyan, colorReset, colorGray, colorReset), m.width)...)

	for i, it := range m.modelItems {
		marker, style := "  ", colorGray
		if i == m.modelIdx {
			marker, style = "> ", colorYellow+colorBold
		}
		cur := ""
		if it.isCurrent {
			cur = " (current)"
		}
		size := humanSize(it.sizeBytes)
		ram := ""
		if it.info != nil {
			ram = fmt.Sprintf("  %d+ GB RAM", it.info.MinRAM)
			if it.info.SizeMB > 0 {
				size = fmt.Sprintf("~%.1f GB", float64(it.info.SizeMB)/1024)
			}
		} else if size == "" {
			size = "installed"
		} else {
			size += " (installed)"
		}
		line := fmt.Sprintf("%s%s%s%s %s%s%s", style, marker, it.name, colorReset, colorGray, size+ram+cur, colorReset)
		rows = append(rows, hardWrap(fitWidth(line, m.width), m.width)...)
	}

	if m.modelBusy {
		bar := progressBar(m.modelProgress)
		rows = append(rows, hardWrap(fmt.Sprintf("%s%s%s %s%s", colorGray, bar, colorReset, m.modelStatus, colorReset), m.width)...)
	} else {
		rows = append(rows, hardWrap(fmt.Sprintf("%s↑/↓ pick, %sEnter%s load/download, %sEsc%s close%s",
			colorGray, colorYellow, colorReset, colorYellow, colorReset, colorReset), m.width)...)
	}
	return rows
}

func progressBar(pct int) string {
	const total = 20
	filled := 0
	if pct > 0 {
		filled = pct * total / 100
	}
	if filled > total {
		filled = total
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", total-filled)
	if pct < 0 {
		return bar + " …"
	}
	return bar + fmt.Sprintf(" %3d%%", pct)
}

func humanSize(b int64) string {
	if b <= 0 {
		return ""
	}
	const gb = 1024 * 1024 * 1024
	if b >= gb {
		return fmt.Sprintf("%.1f GB", float64(b)/gb)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/(1024*1024))
}

// menuCmd продолжает получать прогресс, пока выполняется скачивание/загрузка.
func (m tuiModel) menuCmd() tea.Cmd {
	if m.modelBusy && m.modelCh != nil && m.modelDoneCh != nil {
		return waitForModel(m.modelCh, m.modelDoneCh)
	}
	return nil
}

// handleModelMenuKey обрабатывает клавиши в состоянии меню моделей.
func (m tuiModel) handleModelMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Keystroke() == "ctrl+c" {
		if m.modelCancel != nil && m.modelBusy {
			m.modelCancel()
			m.modelStatus = "cancelling…"
			return m, m.menuCmd()
		}
		return m.closeModelMenu(), nil
	}
	switch {
	case msg.Code == tea.KeyEscape:
		if m.modelBusy {
			return m, m.menuCmd()
		}
		return m.closeModelMenu(), nil
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		if n := len(m.modelItems); n > 0 {
			m.modelIdx = (m.modelIdx - 1 + n) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyDown || msg.Keystroke() == "ctrl+n":
		if n := len(m.modelItems); n > 0 {
			m.modelIdx = (m.modelIdx + 1) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyTab:
		if n := len(m.modelItems); n > 0 {
			m.modelIdx = (m.modelIdx + 1) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyEnter:
		return m.menuEnter()
	}
	if m.modelBusy {
		return m, m.menuCmd()
	}
	m.state = tuiIdle
	if isInputKey(msg) {
		m.input = insertAt(m.input, m.cursorPos, msg.Text)
		m.cursorPos += runeSliceLen(msg.Text)
		m.tabMatches = nil
	}
	return m, nil
}

func (m *tuiModel) closeModelMenu() tuiModel {
	m.state = tuiIdle
	m.modelBusy = false
	m.modelProgress = -1
	m.modelStatus = ""
	return *m
}

// menuEnter выбирает пункт: установленную модель — сразу грузит, рекомендуемую
// — скачивает, а затем тоже грузит.
func (m tuiModel) menuEnter() (tea.Model, tea.Cmd) {
	if len(m.modelItems) == 0 || m.modelBusy {
		return m, nil
	}
	if m.modelItems[m.modelIdx].installed {
		return m.startModelLoad(m.modelItems[m.modelIdx].name)
	}
	return m.startModelDownload(m.modelItems[m.modelIdx].name)
}

func (m tuiModel) startModelDownload(name string) (tea.Model, tea.Cmd) {
	var info *model.ModelInfo
	for i := range model.RecommendedModels {
		if model.RecommendedModels[i].Name == name {
			info = &model.RecommendedModels[i]
			break
		}
	}
	if info == nil {
		m.addLine(fmt.Sprintf("%sunknown recommended model: %s%s", colorRed, name, colorReset))
		return m, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.modelCancel = cancel
	m.modelBusy = true
	m.modelProgress = -1
	m.modelStatus = "downloading " + name
	m.modelCh = make(chan modelProgMsg, 1)
	m.modelDoneCh = make(chan modelDoneMsg, 1)

	d := model.New("")
	cfg := *info
	go func() {
		path, err := d.DownloadCtx(ctx, cfg, func(dl, total int) {
			pct := -1
			if total > 0 {
				pct = dl * 100 / total
			}
			select {
			case m.modelCh <- modelProgMsg{pct: pct}:
			default:
			}
		})
		m.modelDoneCh <- modelDoneMsg{path: path, downloaded: true, err: err}
	}()

	return m, waitForModel(m.modelCh, m.modelDoneCh)
}

func (m tuiModel) startModelLoad(name string) (tea.Model, tea.Cmd) {
	path := model.New("").ModelPath(name)
	ctx, cancel := context.WithCancel(context.Background())
	m.modelCancel = cancel
	m.modelBusy = true
	m.modelProgress = -1
	m.modelStatus = "loading " + name
	m.modelCh = make(chan modelProgMsg, 1)
	m.modelDoneCh = make(chan modelDoneMsg, 1)

	go func() {
		var err error
		if ctx.Err() != nil {
			err = context.Canceled
		} else {
			err = m.s.switchModel(path)
		}
		m.modelDoneCh <- modelDoneMsg{path: path, loaded: true, err: err}
	}()

	return m, waitForModel(m.modelCh, m.modelDoneCh)
}
