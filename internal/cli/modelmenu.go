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
	verified   bool     // проверка целостности завершена
	removed    bool     // модель удалена
	pruned     bool     // недокачанные .tmp удалены
	status     string   // текст результата проверки
	names      []string // удалённые .tmp
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
// modelItemNote СЃРѕР±РёСЂР°РµС‚ РїРѕРґРїРёСЃСЊ СЃРїСЂР°РІР° РѕС‚ РёРјРµРЅРё РјРѕРґРµР»Рё. Р”Р»СЏ СЃРєР°С‡Р°РЅРЅС‹С…
// Р»РѕРєР°Р»СЊРЅС‹С… С„Р°Р№Р»РѕРІ РїРѕРєР°Р·С‹РІР°РµРј СЂР°Р·РјРµСЂ Рё С…РµС€ РёР· .sha256 (РµСЃР»Рё РµСЃС‚СЊ), РґР»СЏ
// РєР°С‚Р°Р»РѕРіР° вЂ” Р·Р°РјРµС‚РєСѓ РїСЂРѕРІР°Р№РґРµСЂР°, РґР»СЏ СЂРµРєРѕРјРµРЅРґР°С†РёР№ вЂ” РїСЂРёРјРµСЂРЅС‹Р№ СЂР°Р·РјРµСЂ Рё RAM.
func modelItemNote(it modelMenuItem) string {
	if it.remote {
		if it.note != "" {
			return it.note
		}
		return "provider model"
	}
	if it.installed {
		size := humanSize(it.sizeBytes)
		hash := localModelHash(it.name)
		switch {
		case size != "" && hash != "":
			return size + ", " + hash
		case hash != "":
			return hash
		case size != "":
			return size
		}
		return "installed"
	}
	size := humanSize(it.sizeBytes)
	ram := ""
	if it.info != nil {
		if it.info.MinRAM > 0 {
			ram = fmt.Sprintf(", %d+ GB RAM", it.info.MinRAM)
		}
		if it.info.SizeMB > 0 {
			size = fmt.Sprintf("~%.1f GB", float64(it.info.SizeMB)/1024)
		}
	}
	if size == "" {
		size = "not installed"
	}
	return size + ram
}

// localModelHash С‡РёС‚Р°РµС‚ sidecar СЂСЏРґРѕРј СЃ РјРѕРґРµР»СЊСЋ Рё РІРѕР·РІСЂР°С‰Р°РµС‚ РєРѕСЂРѕС‚РєСѓСЋ РјРµС‚РєСѓ.
// РџРѕР»РЅСѓСЋ РїСЂРѕРІРµСЂРєСѓ СЃРѕРґРµСЂР¶РёРјРѕРіРѕ РґРµР»Р°РµС‚ verify (F5): Р·РґРµСЃСЊ РІР°Р¶РЅР° РїРѕРґСЃРєР°Р·РєР°, Р° РЅРµ
// С‡С‚РµРЅРёРµ РіРёРіР°Р±Р°Р№С‚РѕРІ РЅР° РєР°Р¶РґС‹Р№ РєР°РґСЂ.
func localModelHash(name string) string {
	path := model.New("").ModelPath(name)
	sum, err := model.ReadSHA256Sidecar(model.SHA256SidecarPath(path))
	if err != nil || sum == "" {
		return "no checksum"
	}
	if len(sum) > 12 {
		sum = sum[:12] + "вЂ¦"
	}
	return "sha256:" + sum
}

// menuMaxVisible — сколько моделей помещается в меню. Список едет за
// выделением через scrollWindow: каталог провайдера длиннее экрана, и
// выбранная модель обязана быть видна.
const menuMaxVisible = 10

// menuWindow — полуинтервал показанных моделей. Считается отдельно от
// отрисовки, но теми же числами: по нему клик сопоставляет строку экрана с
// моделью.
func (m tuiModel) menuWindow() (start, end int) {
	return scrollWindow(m.modelIdx, len(m.visibleModelItems()), menuMaxVisible)
}

// menuRows — строки, показываемые вместо строки ввода, когда открыто меню.
// Второе значение — номер строки первого пункта в срезе. Каждая строка
// ровно одна, поэтому по этому номеру считаются и выделение, и попадание
// клика.
func (m tuiModel) menuRows() ([]string, int) {
	title := "model menu"
	items := m.visibleModelItems()

	rows := make([]string, 0, len(items)+3)
	// Строки меню обрезаются по ширине, а не переносятся: перенос занял бы
	// две строки и сдвинул бы выделение с кликом на соседний пункт.
	rows = append(rows, fitWidth(fmt.Sprintf("%s=== %s%s%s (ctrl+o) ===%s",
		colorBold+colorCyan, title, colorReset, colorGray, colorReset), m.width))

	if m.modelFilter != "" {
		rows = append(rows, fitWidth(fmt.Sprintf("%sfilter:%s %s",
			colorGray, colorGreen, m.modelFilter)+colorReset, m.width))
	}
	first := len(rows)
	start, end := m.menuWindow()
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
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s%s %s%s%s",
			style, marker, it.name, colorReset, colorGray, note+cur, colorReset), m.width))
	}
	rows = append(rows, m.listFooter(start, end, len(items), menuMaxVisible)...)

	switch {
	case m.modelBusy:
		bar := progressBar(m.modelProgress)
		rows = append(rows, fitWidth(fmt.Sprintf("%s%s%s %s%s", colorGray, bar, colorReset, m.modelStatus, colorReset), m.width))
	case len(items) == 0 && m.modelStatus == "":
		rows = append(rows, fitWidth(fmt.Sprintf("%snothing found%s", colorGray, colorReset), m.width))
	default:
		action := "load/download"
		if m.remoteMenu() {
			action = "switch"
		}
		hint := fmt.Sprintf("%s↑/↓ pick, type to filter, %sEnter%s %s, %sEsc%s close%s",
			colorGray, colorYellow, colorReset, action, colorYellow, colorReset, colorReset)
		if !m.remoteMenu() {
			hint = fmt.Sprintf("%s↑/↓ pick, type to filter, %sEnter%s %s, %sDel%s remove, %sF5%s verify, %sF6%s prune, %sEsc%s close%s",
				colorGray,
				colorYellow, colorReset, action,
				colorYellow, colorReset,
				colorYellow, colorReset,
				colorYellow, colorReset,
				colorYellow, colorReset, colorReset)
		}
		rows = append(rows, fitWidth(hint, m.width))
	}
	return rows, first
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
