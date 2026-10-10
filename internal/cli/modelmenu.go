package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/model"
)

// modelMenuItem — одна строка меню моделей (Ctrl+O).
type modelMenuItem struct {
	name      string
	info      *model.ModelInfo // задан для рекомендуемых, ещё не скачанных
	installed bool
	isCurrent bool
	sizeBytes int64
	// remote отличает модели удалённого провайдера от локальных файлов: у
	// них нет размера на диске, и их не «загружают», а переключают.
	remote bool
	// note — необязательная подпись провайдера (владелец модели).
	note string
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

// remoteModelsMsg — каталог моделей удалённого провайдера. Ошибка приходит
// тем же сообщением: список либо получен, либо нет.
type remoteModelsMsg struct {
	models []llm.RemoteModel
	err    error
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

// openModelMenu открывает меню моделей. Удалённый провайдер показывает свой
// каталог, поэтому список приходится запрашивать: у Pollinations их сотни,
// и вбивать имя руками — не способ.
func (m tuiModel) openModelMenu() (tuiModel, tea.Cmd) {
	m.state = tuiModelMenu
	m.modelFilter = ""
	m.modelIdx = 0
	m.modelItems = nil

	if m.remoteMenu() {
		m.modelBusy = true
		m.modelProgress = -1
		m.modelStatus = "loading the model list from " + string(m.provider()) + "…"
		ctx, cancel := context.WithCancel(context.Background())
		m.modelCancel = cancel
		cfg := m.s.cfg
		provider := m.provider()
		// Каталог возвращается сообщением, а не кладётся в канал: bubbletea
		// доставляет в Update только возврат команды, а канал без читателя
		// оставлял меню висеть на «loading…» навсегда.
		return m, func() tea.Msg {
			models, err := llm.ListRemoteModels(llm.Params{
				Provider:      toLLMProvider(provider),
				RemoteModel:   cfg.RemoteModel,
				RemoteBaseURL: cfg.RemoteBaseURL,
				APIKey:        cfg.APIKey(),
				Proxy:         cfg.Proxy(),
			})
			if ctx.Err() != nil {
				err = context.Canceled
			}
			return remoteModelsMsg{models: models, err: err}
		}
	}

	m.modelItems = m.buildModelMenu()
	for i, it := range m.modelItems {
		if it.isCurrent {
			m.modelIdx = i
		}
	}
	return m, nil
}

// provider — провайдер, на котором реально работает сессия.
func (m tuiModel) provider() config.Provider {
	return currentProvider(m.s, m.rf.cfg.Provider)
}

// localModelPath — путь к локальной модели: та же цепочка, что в /model и в
// статусной строке.
func (m tuiModel) localModelPath() string {
	if m.s != nil && m.s.cfg.ModelPath != "" {
		return m.s.cfg.ModelPath
	}
	return m.rf.cfg.ModelPath
}

// remoteMenu сообщает, что меню должно показать каталог удалённого
// провайдера, а не локальные файлы. Правило то же, что в /model: auto с
// локальной моделью — это локальные файлы.
func (m tuiModel) remoteMenu() bool {
	return showsRemoteProvider(m.provider(), m.localModelPath())
}

// buildRemoteMenu превращает каталог провайдера в строки меню. Текущая
// модель помечается, чтобы её нельзя было выбрать случайно дважды.
func (m tuiModel) buildRemoteMenu(models []llm.RemoteModel) []modelMenuItem {
	cur := strings.TrimSpace(m.s.cfg.RemoteModel)
	items := make([]modelMenuItem, 0, len(models))
	for _, rm := range models {
		items = append(items, modelMenuItem{
			name:      rm.ID,
			note:      rm.Note,
			remote:    true,
			installed: true,
			isCurrent: rm.ID == cur,
		})
	}
	return items
}

// applyRemoteModelsMsg кладёт каталог в меню. Ошибка остаётся на экране, а
// не теряется: «сервер не запущен» и «нужен ключ» пользователю нужны.
func (m *tuiModel) applyRemoteModelsMsg(msg remoteModelsMsg) {
	m.modelBusy = false
	if m.modelCancel != nil {
		m.modelCancel()
		m.modelCancel = nil
	}
	m.modelProgress = -1
	if msg.err != nil {
		m.modelStatus = colorRed + msg.err.Error() + colorReset
		return
	}
	m.modelStatus = ""
	m.modelItems = m.buildRemoteMenu(msg.models)
	m.modelIdx = 0
	for i, it := range m.modelItems {
		if it.isCurrent {
			m.modelIdx = i
		}
	}
	if len(m.modelItems) == 0 {
		m.modelStatus = colorGray + "the provider reported no models" + colorReset
	}
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

// visibleModelItems — строки меню после фильтра. Фильтр обязателен:
// каталог Pollinations длинный, и искать его стрелками невозможно.
func (m tuiModel) visibleModelItems() []modelMenuItem {
	f := strings.ToLower(strings.TrimSpace(m.modelFilter))
	if f == "" {
		return m.modelItems
	}
	out := make([]modelMenuItem, 0, len(m.modelItems))
	for _, it := range m.modelItems {
		if strings.Contains(strings.ToLower(it.name), f) {
			out = append(out, it)
		}
	}
	return out
}

// modelItemNote собирает подпись справа от имени модели. У локальной это
// размер файла и требуемая память, у удалённой — владелец из каталога.
func modelItemNote(it modelMenuItem) string {
	if it.remote {
		if it.note != "" {
			return it.note
		}
		return "provider model"
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
	return size + ram
}

// menuRows — строки, показываемые вместо строки ввода, когда открыто меню.
func (m tuiModel) menuRows() []string {
	title := "model menu"
	items := m.visibleModelItems()

	rows := make([]string, 0, len(items)+3)
	rows = append(rows, hardWrap(fmt.Sprintf("%s=== %s%s%s (ctrl+o) ===%s",
		colorBold+colorCyan, title, colorReset, colorGray, colorReset), m.width)...)

	if m.modelFilter != "" {
		rows = append(rows, hardWrap(fitWidth(fmt.Sprintf("%sfilter:%s %s",
			colorGray, colorGreen, m.modelFilter)+colorReset, m.width), m.width)...)
	}
	// Окно едет за выделением: каталог длиннее экрана, и выбранная модель
	// обязана быть видна.
	const maxVisible = 10
	start, end := scrollWindow(m.modelIdx, len(items), maxVisible)
	for i := start; i < end; i++ {
		it := items[i]
		marker, style := "  ", colorGray
		if i == m.modelIdx {
			marker, style = "> ", colorYellow+colorBold
		}
		cur := ""
		if it.isCurrent {
			cur = " (current)"
		}
		note := modelItemNote(it)
		rows = append(rows, hardWrap(fitWidth(fmt.Sprintf("%s%s%s%s %s%s%s",
			style, marker, it.name, colorReset, colorGray, note+cur, colorReset), m.width), m.width)...)
	}
	rows = append(rows, m.listFooter(start, end, len(items), maxVisible)...)

	switch {
	case m.modelBusy:
		bar := progressBar(m.modelProgress)
		rows = append(rows, hardWrap(fmt.Sprintf("%s%s%s %s%s", colorGray, bar, colorReset, m.modelStatus, colorReset), m.width)...)
	case len(items) == 0 && m.modelStatus == "":
		rows = append(rows, hardWrap(fmt.Sprintf("%snothing found%s", colorGray, colorReset), m.width)...)
	default:
		action := "load/download"
		if m.remoteMenu() {
			action = "switch"
		}
		rows = append(rows, hardWrap(fmt.Sprintf("%s↑/↓ pick, type to filter, %sEnter%s %s, %sEsc%s close%s",
			colorGray, colorYellow, colorReset, action, colorYellow, colorReset, colorReset), m.width)...)
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
// Печатные клавиши фильтруют список, а не закрывают окно: каталог
// провайдера длинный, и искать его иначе никак.
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
