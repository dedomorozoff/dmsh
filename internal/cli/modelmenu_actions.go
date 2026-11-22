package cli

import (
	"context"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/model"
)

func (m tuiModel) handleModelMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Keystroke() == "ctrl+c" {
		if m.modelCancel != nil && m.modelBusy {
			m.modelCancel()
			m.modelStatus = "cancelling…"
			return m, m.menuCmd()
		}
		return m.closeModelMenu(), nil
	}
	items := m.visibleModelItems()
	switch {
	case msg.Code == tea.KeyEscape:
		if m.modelBusy {
			return m, m.menuCmd()
		}
		// Esc сначала снимает фильтр: закрывать окно, потеряв список,
		// неудобно.
		if m.modelFilter != "" {
			m.modelFilter = ""
			m.modelIdx = 0
			return m, nil
		}
		return m.closeModelMenu(), nil
	case msg.Code == tea.KeyBackspace || msg.Keystroke() == "ctrl+h":
		if m.modelBusy {
			return m, m.menuCmd()
		}
		if r := []rune(m.modelFilter); len(r) > 0 {
			m.modelFilter = string(r[:len(r)-1])
			m.modelIdx = 0
		}
		return m, nil
	case msg.Code == tea.KeyUp || msg.Keystroke() == "ctrl+p":
		if n := len(items); n > 0 {
			m.modelIdx = (m.modelIdx - 1 + n) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyDown || msg.Keystroke() == "ctrl+n":
		if n := len(items); n > 0 {
			m.modelIdx = (m.modelIdx + 1) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyTab:
		if n := len(items); n > 0 {
			m.modelIdx = (m.modelIdx + 1) % n
		}
		return m, m.menuCmd()
	case msg.Code == tea.KeyEnter:
		return m.menuEnter()
	case msg.Code == tea.KeyDelete:
		if m.modelBusy || m.remoteMenu() {
			return m, m.menuCmd()
		}
		return m.menuRemove(items)
	case msg.Code == tea.KeyF5:
		if m.modelBusy || m.remoteMenu() {
			return m, m.menuCmd()
		}
		return m.menuVerify(items)
	case msg.Code == tea.KeyF6:
		if m.modelBusy || m.remoteMenu() {
			return m, m.menuCmd()
		}
		return m.menuPrune()
	}
	if m.modelBusy {
		return m, m.menuCmd()
	}
	if isInputKey(msg) {
		m.modelFilter += msg.Text
		m.modelIdx = 0
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) closeModelMenu() tuiModel {
	m.state = tuiIdle
	m.modelBusy = false
	m.modelProgress = -1
	m.modelStatus = ""
	m.modelFilter = ""
	m.modelItems = nil
	if m.modelCancel != nil {
		m.modelCancel()
		m.modelCancel = nil
	}
	return *m
}

// menuEnter выбирает пункт: у локальной модели это загрузка файла или
// скачивание, у удалённой — переключение движка на другую модель.
func (m tuiModel) menuEnter() (tea.Model, tea.Cmd) {
	items := m.visibleModelItems()
	if len(items) == 0 || m.modelBusy {
		return m, nil
	}
	it := items[m.modelIdx]
	if it.remote {
		return m.applyRemoteModel(it.name), nil
	}
	if it.installed {
		return m.startModelLoad(it.name)
	}
	return m.startModelDownload(it.name)
}

// applyRemoteModel переключает удалённого провайдера на выбранную модель и
// печатает, что менять дальше: движок пересобран для этой сессии, а в файл
// модель попадёт только через setup → apply, как и у локальной.
func (m tuiModel) applyRemoteModel(name string) tuiModel {
	if err := m.s.setRemoteModel(name); err != nil {
		m.addLine(fmt.Sprintf("%smodel %s: %v%s", colorRed, name, err, colorReset))
		return m
	}
	m.modelStatus = ""
	for i := range m.modelItems {
		m.modelItems[i].isCurrent = m.modelItems[i].name == name
	}
	m.addLine(fmt.Sprintf("%s[dmsh]%s model: %s%s %s(session only — Ctrl+P → setup → apply to keep it)%s",
		colorCyan, colorReset, name, colorReset, colorGray, colorReset))
	return m
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

// menuRemove удаляет выбранный скачанный .gguf и .sha256 рядом с ним.
// Рекомендованную, но ещё не скачанную модель удалить нельзя — её нет на диске.
func (m tuiModel) menuRemove(items []modelMenuItem) (tea.Model, tea.Cmd) {
	if len(items) == 0 || m.modelIdx < 0 || m.modelIdx >= len(items) {
		return m, nil
	}
	it := items[m.modelIdx]
	if !it.installed || it.remote {
		return m, nil
	}
	d := model.New("")
	path := d.ModelPath(it.name)
	if err := removeLocalModelFile(d, it.name); err != nil {
		m.addLine(fmt.Sprintf("%sremove %s: %v%s", colorRed, it.name, err, colorReset))
		return m, nil
	}
	// Если удалили текущую модель, сессия больше не должна считать её активной.
	if m.s != nil && filepath.Base(m.s.cfg.ModelPath) == it.name {
		m.s.cfg.ModelPath = ""
		if m.rf != nil {
			m.rf.cfg.ModelPath = ""
		}
	}
	m.modelItems = m.buildModelMenu()
	vis := m.visibleModelItems()
	if m.modelIdx >= len(vis) {
		m.modelIdx = len(vis) - 1
	}
	if m.modelIdx < 0 {
		m.modelIdx = 0
	}
	m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, it.name))
	_ = path
	return m, nil
}

// menuPrune удаляет недокачанные *.gguf.tmp из каталога моделей.
func (m tuiModel) menuPrune() (tea.Model, tea.Cmd) {
	d := model.New("")
	removed, err := pruneIncompleteModelFiles(d.ModelPath(""))
	if err != nil {
		m.addLine(fmt.Sprintf("%sprune: %v%s", colorRed, err, colorReset))
		return m, nil
	}
	m.modelItems = m.buildModelMenu()
	vis := m.visibleModelItems()
	if m.modelIdx >= len(vis) {
		m.modelIdx = len(vis) - 1
	}
	if m.modelIdx < 0 {
		m.modelIdx = 0
	}
	if len(removed) == 0 {
		m.addLine("(nothing to prune)")
		return m, nil
	}
	for _, name := range removed {
		m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, name))
	}
	return m, nil
}

// menuVerify сверяет выбранную модель с .sha256 в фоне: файл большой, и ждать
// в цикле событий нельзя.
func (m tuiModel) menuVerify(items []modelMenuItem) (tea.Model, tea.Cmd) {
	if len(items) == 0 || m.modelIdx < 0 || m.modelIdx >= len(items) {
		return m, nil
	}
	it := items[m.modelIdx]
	if !it.installed || it.remote {
		return m, nil
	}
	path := model.New("").ModelPath(it.name)
	ctx, cancel := context.WithCancel(context.Background())
	m.modelCancel = cancel
	m.modelBusy = true
	m.modelProgress = -1
	m.modelStatus = "verifying " + it.name
	m.modelCh = make(chan modelProgMsg, 1)
	m.modelDoneCh = make(chan modelDoneMsg, 1)

	go func() {
		if ctx.Err() != nil {
			m.modelDoneCh <- modelDoneMsg{path: path, verified: true, err: context.Canceled}
			return
		}
		ok, status := verifyModelStatus(path)
		if ok {
			status = "OK (sha256 verified)"
		} else {
			switch status {
			case "no checksum":
				status = "NO CHECKSUM (missing .sha256 sidecar)"
			case "checksum mismatch":
				status = "FAIL (sha256 mismatch)"
			default:
				status = "FAIL (" + status + ")"
			}
		}
		m.modelDoneCh <- modelDoneMsg{path: path, verified: true, status: status}
	}()

	return m, waitForModel(m.modelCh, m.modelDoneCh)
}
