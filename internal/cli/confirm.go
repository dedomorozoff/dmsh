package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/dedomorozoff/dmsh/internal/prompt"
)

// confirm спрашивает пользователя y/n. Возвращает true только при явном yes.
// Если пользователь вводит slash-команду, она обрабатывается и возвращается ErrSlashCommand.
func confirm(r LineReader, out io.Writer, s *session, promptText string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", promptText)
	answer, err := r.ReadLine()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	answer = strings.TrimSpace(answer)

	// If user enters a slash command during confirmation, handle it
	if strings.HasPrefix(answer, "/") {
		if stop := handleSlash(answer, out, s); stop {
			return false, context.Canceled
		}
		return false, ErrSlashCommand
	}

	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

// Окно подтверждения в TUI. Отдельный файл от confirm(): там построчный
// вопрос для пайпа и REPL, здесь — окно с кнопками для мыши.

// Подтверждение выполнения — окно, а не строка в скроллбеке. Причина в том,
// что вопрос «точно ли это надо?» без ответа выглядит как «да»: пользователь
// жмёт Enter по привычке и запускает команду, которую не читал. Окно показывает
// саму команду, уровень риска и правило, которое сработало, — а кнопки
// одинаковы для мыши и для клавиатуры.

// Строки окна подтверждения. Значение константы — индекс строки, по нему же
// считаются и клик, и курсор.
const (
	confirmRowRisk = iota
	confirmRowCommand
	confirmRowWhy
	confirmRowRun
	confirmRowCancel
	confirmRowCount
)

const confirmTitle = "execute"

// confirmRisk — уровень риска команды, которую предлагают выполнить.
func (m tuiModel) confirmRisk() prompt.Risk {
	if m.pendingResp.Risk != "" {
		return m.pendingResp.Risk
	}
	return prompt.RiskMedium
}

// confirmWhy — почему нужно подтверждение. Пустая причина означает «модель
// сама попросила», и это тоже нужно сказать: иначе непонятно, кто автор
// вопроса.
func (m tuiModel) confirmWhy() string {
	if m.confirmReason != "" {
		return m.confirmReason
	}
	return "the model asked to confirm this command"
}

func (m tuiModel) confirmRow(row int) string {
	switch row {
	case confirmRowRisk:
		risk := m.confirmRisk()
		return riskColor(risk) + string(risk) + " risk · needs confirmation" + colorReset
	case confirmRowCommand:
		// Подсветка опций и флагов: опасное обычно живёт именно в них.
		return fitWidth(fmt.Sprintf("  $ %s%s", renderCommand(m.pendingResp.Command), colorReset), m.width)
	case confirmRowWhy:
		return fitWidth(fmt.Sprintf("  why: %s%s%s", colorGray, m.confirmWhy(), colorReset), m.width)
	case confirmRowRun:
		return fmt.Sprintf("  %s[ run ]%s      %sy%s", colorYellow+colorBold, colorReset, colorGray, colorReset)
	case confirmRowCancel:
		return fmt.Sprintf("  %s[ cancel ]%s    %sn / Esc%s", colorGray, colorReset, colorGray, colorReset)
	}
	return ""
}

func (m tuiModel) confirmRows() []string {
	rows := make([]string, 0, confirmRowCount)
	for i := 0; i < confirmRowCount; i++ {
		rows = append(rows, m.confirmRow(i))
	}
	return rows
}

// confirmModal описывает окно подтверждения. Курсор стоит на «run»: вводом
// он и выбирается (Enter = отмена, y = выполнить), и мышью.
func (m tuiModel) confirmModal() modalBox {
	return modalBox{title: confirmTitle, rows: m.confirmRows(), cursorX: 3, cursorY: confirmRowRun}
}

// openConfirm спрашивает пользователя перед запуском команды. Причина и риск
// сохраняются: окно должно объяснять, почему вопрос вообще возник.
func (m tuiModel) openConfirm(resp prompt.Response, dec policyDecisionLite) tuiModel {
	m.state = tuiConfirming
	m.pendingResp = resp
	m.confirmReason = dec.reason
	m.confirmText = ""
	m.confirmOK = nil
	m.input = ""
	m.cursorPos = 0
	return m
}

// handleConfirmKey обрабатывает клавиши в окне подтверждения.
func (m tuiModel) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmed := false
	switch msg.String() {
	case "y", "Y", "yes":
		confirmed = true
	case "n", "N", "no", "enter":
		confirmed = false
	case "ctrl+c", "esc":
		confirmed = false
		m.input = ""
		m.cursorPos = 0
	default:
		return m, nil
	}
	return m.applyConfirm(confirmed)
}

// applyConfirm выполняет или отменяет команду. Один путь и для клавиш, и для
// кнопок: подтверждение не должно зависеть от того, чем именно его выбрали.
func (m tuiModel) applyConfirm(confirmed bool) (tea.Model, tea.Cmd) {
	m.state = tuiIdle
	m.confirmReason = ""
	if m.confirmOK != nil {
		m.confirmOK(confirmed)
		m.confirmOK = nil
	}
	if confirmed && m.pendingResp.Intent == prompt.IntentRunCommand {
		return m.runCommand(m.pendingResp)
	}
	if !confirmed {
		m.addLine("(cancelled)")
	}
	return m, nil
}

// confirmClick — что делает клик по строке окна. Только две нижние строки:
// команда и причина — текст для чтения, а не кнопки.
func (m tuiModel) confirmClick(row int) (clickAction, int) {
	switch row {
	case confirmRowRun:
		return clickConfirmRun, 0
	case confirmRowCancel:
		return clickConfirmCancel, 0
	}
	return clickNone, 0
}
