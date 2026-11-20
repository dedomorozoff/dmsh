package cli

import (
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/executor"
	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
	"context"
	"fmt"
	"path/filepath"
)

type tuiState int

const (
	tuiIdle tuiState = iota
	tuiStreaming
	tuiConfirming
	tuiQuestion
	tuiSearch
	tuiModelMenu
	tuiPalette
	tuiSettings
	tuiSetup
	tuiModeMenu
	tuiDirMenu  // выбор папки кликом по текущей папке в статусной строке
	tuiTodoMenu // список задач модели с чекбоксами
	tuiTerminal // терминал в окне приложения: экран занимает весь кадр
	tuiRunning  // команда выполняется: ввод свободен, виден спиннер
	tuiFind     // поиск по выводу: запрос в строке ввода, совпадения подсвечены
)

type tuiModel struct {
	rf   *rootFlags
	s    *session
	errW io.Writer

	input     string
	cursorPos int
	history   []string
	histIdx   int

	content   string
	modeLabel string
	// scrollbackLines — сколько строк в content с последней обрезки. Сам
	// транскрипт не ограничен: его обрезает scrollback.go.
	scrollbackLines int
	// bodyCache — транскрипт, уже свёрнутый по ширине. Указатель, потому что
	// модель копируется по значению: кеш должен переживать копирование.
	bodyCache *bodyWrapCache

	width  int
	height int

	state            tuiState
	streaming        bool
	streamed         bool
	streamHasContent bool
	streamCtx        context.Context
	streamCancel     context.CancelFunc
	tokenCh          chan tokenMsg
	streamDone       chan streamResult

	confirmText string
	confirmOK   func(bool)
	// confirmReason — правило, которое потребовало подтверждения. Без него
	// окно спрашивает «точно ли?», не объясняя, кто и почему.
	confirmReason string
	pendingResp   prompt.Response
	// clarify ведёт диалог уточнений текущего запроса: модель не должна
	// спрашивать одно и то же и не должна спрашивать бесконечно.
	clarify clarification

	searchQuery string
	searchIdx   int
	// findQuery — поиск по выводу. Живёт и после закрытия окна поиска:
	// подсветка остаётся, пока её не снимут Esc, а F3 листает совпадения.
	findQuery   string
	findMatches []int
	findIdx     int

	tabMatches []string
	tabIdx     int

	// command palette (Ctrl+P): запрос живёт в обычной строке ввода,
	// paletteSaved — то, что там было до открытия.
	paletteSaved string
	paletteIdx   int

	// model menu (Ctrl+O)
	modelItems    []modelMenuItem
	modelIdx      int
	modelBusy     bool
	modelStatus   string
	modelProgress int
	modelCancel   context.CancelFunc
	modelCh       chan modelProgMsg
	modelDoneCh   chan modelDoneMsg
	// modelFilter — подстрока поиска по списку моделей.
	modelFilter string

	// settings screen (Ctrl+P → settings): правки прокси и endpoint'а.
	// Применяются только по Enter на строке save.
	setIdx       int
	setMode      netproxy.Mode
	setProto     string
	setHost      string
	setPort      string
	setLogin     string
	setPass      string
	setNoProxy   string
	setEndpoint  string
	setEditing   bool
	setBusy      bool
	setStatus    string
	setStatusErr bool
	setCancel    context.CancelFunc

	// setup screen (Ctrl+P → setup): выбор способа подключения к LLM.
	// Применяется по Enter на строке apply.
	supIdx      int
	supProvider config.Provider
	supRemote   string
	supLocal    string
	supEndpoint string
	// supKey — ключ, который будет записан в конфиг. Значение из переменной
	// окружения сюда не попадает: переносить секрет в файл без явного
	// желания нельзя.
	supKey       string
	supEditing   bool
	supStatus    string
	supStatusErr bool

	// dirMenu — выбор папки (клик по папке в статусной строке). Список
	// каталогов перечитывается при каждом переходе, поэтому держим его
	// рядом с состоянием, а не кешируем на сессию.
	dirItems  []dirItem
	dirIdx    int
	dirFilter string
	dirErr    string

	// todoIdx — выделенная задача в окне задач модели.
	todoIdx int

	// actionCmd — команда последнего выполнения: после неё dmsh предлагает,
	// что делать дальше. Пусто — панели под ответом нет.
	actionCmd string

	// term — встроенный терминал режима shell. Не nil, только пока
	// псевдотерминал жив: экран терминала рисуется вместо транскрипта.
	term *terminalPane

	scrollOffset int

	// fixTries считает автокоррекции подряд. Без счётчика модель, которая
	// возвращает ту же падающую команду, запускает бесконечный цикл.
	fixTries int

	// running — фоновая команда (composer, тесты, сборка): пока она идёт,
	// ввод свободен, а состояние держит спиннер. cmdSeq нумерует запуски,
	// чтобы устаревший результат не затёр новый.
	running      bool
	cmdSeq       int
	cmdCancel    context.CancelFunc
	cmdCommand   string
	cmdStartedAt time.Time
	// cmdCh — живые куски вывода фоновой команды и её итог. Один канал на
	// оба: итог обязан прийти после последнего куска, иначе «готово» легло бы
	// поверх недописанного вывода.
	cmdCh chan tea.Msg
	// cmdLive — сколько байт вывода уже показано вживую, cmdTrunc — живой
	// вывод обрезан по лимиту. Итог дублировать уже показанное не должен.
	cmdLive  int
	cmdTrunc bool
}

type streamResult struct {
	resp prompt.Response
	raw  string
	err  error
}

type tokenMsg struct {
	text string
}

type streamDoneMsg struct {
	resp prompt.Response
	err  error
}

// cmdDoneMsg — фоновая команда завершилась. seq отбрасывает устаревшие
// результаты: пользователь мог запустить новую команду раньше.
type cmdDoneMsg struct {
	seq  int
	resp prompt.Response
	res  executor.Result
	dec  policyDecisionLite
}

// cmdOutMsg — кусок вывода фоновой команды, пришедший, пока она ещё пишет.
// Границы кусков не совпадают со строками: текст дописывается как есть.
type cmdOutMsg struct {
	seq   int
	text  string
	isErr bool
}

// cmdTickMsg — секундный тик фоновой команды: обновляет время «идёт…».
type cmdTickMsg struct{}

// directDoneMsg — прямая команда через "!" завершилась в фоне.
type directDoneMsg struct {
	seq int
	cmd string
	res executor.Result
}

// policyDecisionLite — копия решения политики для записи в аудит после
// фонового выполнения: сам объект policy.Decision не храним, чтобы не
// тащить зависимость в сообщения.
type policyDecisionLite struct {
	allowed bool
	risk    prompt.Risk
	reason  string
}

type tuiWriter struct {
	buf     *strings.Builder
	tokenCh chan<- tokenMsg
	last    time.Time
}

func (w *tuiWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	now := time.Now()
	if now.Sub(w.last) > 40*time.Millisecond {
		select {
		case w.tokenCh <- tokenMsg{text: w.buf.String()}:
			w.buf.Reset()
			w.last = now
		default:
		}
	}
	return len(p), nil
}

func NewTuiModel(rf *rootFlags, s *session) tea.Model {
	m := tuiModel{rf: rf, s: s, errW: io.Discard, modeLabel: string(rf.cfg.Mode), content: "", tabIdx: -1, bodyCache: &bodyWrapCache{width: -1}}
	m.history = m.persistedHistory()
	m.histIdx = len(m.history)
	return m
}

// persistedHistory читает историю прошлых сессий из файла. Стрелки вверх/вниз
// листают по ней, поэтому без загрузки файла история была бы видна только
// внутри одной сессии. Дубликаты подряд идущих одинаковых записей убираются:
// один и тот же Enter, повторённый дважды, не должен занимать два шага.
func (m tuiModel) persistedHistory() []string {
	entries, err := loadHistoryEntries(m.s.cfg.HistoryFile)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		cmd := strings.TrimSpace(e.Command)
		if cmd == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == cmd {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

func (m tuiModel) Init() tea.Cmd {
	return tea.RequestWindowSize
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTerminal()
		return m, nil
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg), nil
	case tokenMsg:
		if len(msg.text) > 0 {
			m.streamed = true
			m.streamHasContent = true
			m.appendScrollback(msg.text)
			// Ответ модели прокручивается сам: пока он пишется, пользователь
			// смотрит на него, а не читает лог команды выше.
			m.scrollOffset = 0
		}
		if m.streaming && m.tokenCh != nil {
			return m, waitForToken(m.tokenCh, m.streamDone)
		}
		return m, nil
	case streamDoneMsg:
		m.streaming = false
		m.streamed = false
		m.streamHasContent = false
		m.state = tuiIdle
		m.tokenCh = nil
		m.streamDone = nil
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
		}
		if msg.err != nil {
			m.content = appendLine(m.content, fmt.Sprintf("%sError: %v%s", colorRed, msg.err, colorReset))
			return m, nil
		}
		if msg.resp.Intent == prompt.IntentAskClarification {
			if msg.resp.Command != "" {
				return m.executeResponse(msg.resp)
			}
			question := strings.TrimSpace(msg.resp.Question)
			if question != "" {
				// Модель повторило вопрос или исчерпало круги: останавливаемся,
				// чтобы пользователь не отвечал на одно и то же по кругу.
				if m.clarify.exhausted() || m.clarify.repeatsLast(question) {
					m.addLine(fmt.Sprintf("%s(stopped: the model kept asking instead of answering — rephrase the request)%s", colorGray, colorReset))
					return m, nil
				}
				m.addLine(fmt.Sprintf("%s[dmsh] %s%s", colorCyan, msg.resp.Question, colorReset))
				m.state = tuiQuestion
				m.pendingResp = msg.resp
				m.input = ""
				m.cursorPos = 0
				return m, nil
			}
		}
		if msg.resp.Intent == prompt.IntentRunCommand && msg.resp.Command != "" {
			return m.executeResponse(msg.resp)
		}
		if msg.resp.Intent == prompt.IntentExplain && msg.resp.Command != "" {
			return m.executeResponse(msg.resp)
		}
		return m, nil
	case modelProgMsg:
		m.modelProgress = msg.pct
		if m.modelBusy && m.modelDoneCh != nil {
			return m, waitForModel(m.modelCh, m.modelDoneCh)
		}
		return m, nil
	case proxyTestMsg:
		// Результат проверки приходит в любой момент: если пользователь
		// уже закрыл экран, сообщение просто игнорируем.
		if m.state == tuiSettings {
			m.finishProxyTest(msg)
		} else if m.setCancel != nil {
			m.setCancel()
			m.setCancel = nil
		}
		return m, nil
	case terminalOutputMsg:
		// Вывод приходит кусками и рисуется только пока терминал открыт:
		// закрытую панель незачем кормить.
		if m.term == nil {
			return m, nil
		}
		_, _ = m.term.screen.Write(msg.data)
		return m, waitTerminal(m.term)
	case terminalExitMsg:
		return m.finishTerminal(msg), nil
	case terminalClosedMsg:
		return m, nil
	case cmdDoneMsg:
		return m.finishBackgroundCommand(msg)
	case cmdOutMsg:
		return m.appendCmdOutput(msg)
	case cmdTickMsg:
		// Тик обновляет время в строке ввода. Очередь при этом нужно
		// продолжать вычитывать: тик приходит в тот же канал, что и вывод,
		// и без нового pump он умирает — вместе с ним замирает и команда,
		// и её результат.
		return m, m.cmdPump()
	case directDoneMsg:
		return m.finishDirectCommand(msg)
	case modelDoneMsg:
		m.modelBusy = false
		m.modelProgress = -1
		m.modelStatus = ""
		if m.modelCancel != nil {
			m.modelCancel()
			m.modelCancel = nil
		}
		// Загрузка закончена: в каналы больше никто не пишет, и следующее
		// открытие меню не должно ждать их сообщений впустую.
		m.modelCh = nil
		m.modelDoneCh = nil
		if msg.err != nil {
			if msg.err == context.Canceled {
				m.addLine("(cancelled)")
			} else {
				m.addLine(fmt.Sprintf("%s%s%s", colorRed, msg.err, colorReset))
			}
			return m, nil
		}
		switch {
		case msg.pruned:
			m.modelItems = m.buildModelMenu()
			if m.modelIdx >= len(m.visibleModelItems()) {
				m.modelIdx = len(m.visibleModelItems()) - 1
			}
			if m.modelIdx < 0 {
				m.modelIdx = 0
			}
			if len(msg.names) == 0 {
				m.addLine("(nothing to prune)")
			} else {
				for _, n := range msg.names {
					m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, n))
				}
			}
			return m, nil
		case msg.removed:
			name := filepath.Base(msg.path)
			m.modelItems = m.buildModelMenu()
			vis := m.visibleModelItems()
			if m.modelIdx >= len(vis) {
				m.modelIdx = len(vis) - 1
			}
			if m.modelIdx < 0 {
				m.modelIdx = 0
			}
			m.addLine(fmt.Sprintf("%sRemoved%s %s", colorGray, colorReset, name))
			return m, nil
		case msg.verified:
			name := filepath.Base(msg.path)
			status := msg.status
			if status == "" {
				status = "done"
			}
			if strings.HasPrefix(status, "OK") {
				m.addLine(fmt.Sprintf("%s%s: %s%s", colorGreen, name, status, colorReset))
			} else {
				m.addLine(fmt.Sprintf("%s%s: %s%s", colorYellow, name, status, colorReset))
			}
			return m, nil
		case msg.downloaded:
			name := filepath.Base(msg.path)
			m.addLine(fmt.Sprintf("(downloaded %s)", name))
			return m.startModelLoad(name)
		default:
			name := filepath.Base(msg.path)
			m.modelItems = m.buildModelMenu()
			for i, it := range m.modelItems {
				if it.name == name {
					m.modelIdx = i
				}
			}
			m.addLine(fmt.Sprintf("(%smodel%s: %s)", colorCyan, colorReset, name))
			return m, nil
		}
	case remoteModelsMsg:
		// Каталог приходит после закрытия окна: сообщение просто игнорируем.
		if m.state != tuiModelMenu {
			return m, nil
		}
		m.applyRemoteModelsMsg(msg)
		return m, nil
	}
	return m, nil
}

