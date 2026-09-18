package feedback

import (
	"fmt"
	"strings"
)

// Result представляет результат выполнения команды с анализом.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Success  bool
	Hint     string
}

// Analyze анализирует вывод команды и возвращает понятное объяснение.
func Analyze(command, stdout, stderr string, exitCode int) Result {
	success := exitCode == 0 && stderr == ""

	// Анализируем типичные паттерны
	if !success {
		hint := analyzeError(command, stderr, exitCode)
		return Result{
			ExitCode: exitCode,
			Stdout:   stdout,
			Stderr:   stderr,
			Success:  false,
			Hint:     hint,
		}
	}

	hint := analyzeSuccess(command, stdout)
	return Result{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Success:  true,
		Hint:     hint,
	}
}

// analyzeError анализирует ошибки и даёт рекомендации.
func analyzeError(command, stderr string, exitCode int) string {
	lower := strings.ToLower(stderr)
	_ = command // зарезервировано для будущего использования

	// Файл уже существует
	if strings.Contains(lower, "уже существует") || strings.Contains(lower, "already exists") {
		return "Файл или папка уже существует. Используй другое имя или добавь -Force для перезаписи."
	}

	// Файл не найден
	if strings.Contains(lower, "не найден") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "cannot find") || strings.Contains(lower, "does not exist") {
		return "Файл или папка не найдены. Проверь путь или имя."
	}

	// Доступ запрещён
	if strings.Contains(lower, "доступ запрещен") || strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "permission denied") {
		return "Нет прав доступа. Попробуй запустить с правами администратора."
	}

	// Процесс занят
	if strings.Contains(lower, "используется другим процессом") || strings.Contains(lower, "being used by another process") {
		return "Файл занят другим процессом. Закрой программу, которая его использует."
	}

	// Диск полный
	if strings.Contains(lower, "нет места") || strings.Contains(lower, "disk full") ||
		strings.Contains(lower, "not enough space") {
		return "Недостаточно места на диске. Освободи место и попробуй снова."
	}

	// Синтаксическая ошибка
	if strings.Contains(lower, "syntax") || strings.Contains(lower, "синтаксис") {
		return "Ошибка в синтаксисе команды. Проверь правильность написания."
	}

	// Команда не найдена
	if strings.Contains(lower, "не распознано") || strings.Contains(lower, "not recognized") ||
		strings.Contains(lower, "command not found") {
		return "Команда не найдена. Возможно, нужно установить программу или проверить путь."
	}

	// Таймаут
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return "Превышено время ожидания. Попробуй позже или проверь сеть."
	}

	// Сеть
	if strings.Contains(lower, "network") || strings.Contains(lower, "сеть") ||
		strings.Contains(lower, "connection refused") {
		return "Проблема с сетью. Проверь подключение к интернету."
	}

	// PowerShell конкретные ошибки
	if strings.Contains(stderr, "FullyQualifiedErrorId") {
		if strings.Contains(stderr, "NewItemIOError") {
			return "Ошибка создания элемента. Возможно, путь неверный или файл уже существует."
		}
		if strings.Contains(stderr, "PathNotFound") {
			return "Путь не найден. Проверь, существует ли директория."
		}
	}

	// Общая ошибка
	if exitCode != 0 {
		return fmt.Sprintf("Команда завершилась с кодом %d. Проверь параметры.", exitCode)
	}

	return ""
}

// analyzeSuccess анализирует успешный вывод и даёт контекст.
func analyzeSuccess(command, stdout string) string {
	cmdLower := strings.ToLower(command)

	// Создание файла/папки
	if strings.Contains(cmdLower, "new-item") || strings.Contains(cmdLower, "mkdir") ||
		strings.Contains(cmdLower, "touch") {
		if strings.Contains(stdout, "Directory:") || strings.Contains(stdout, "Mode") {
			return "Готово!"
		}
	}

	// Удаление
	if strings.Contains(cmdLower, "remove-item") || strings.Contains(cmdLower, "rm ") ||
		strings.Contains(cmdLower, "rmdir") || strings.Contains(cmdLower, "del ") {
		return "Удалено успешно."
	}

	// Копирование
	if strings.Contains(cmdLower, "copy-item") || strings.Contains(cmdLower, "cp ") {
		return "Скопировано успешно."
	}

	// Перемещение
	if strings.Contains(cmdLower, "move-item") || strings.Contains(cmdLower, "mv ") {
		return "Перемещено успешно."
	}

	// Список файлов
	if strings.Contains(cmdLower, "get-childitem") || strings.Contains(cmdLower, "dir") ||
		strings.Contains(cmdLower, "ls") {
		lines := strings.Count(stdout, "\n")
		if lines == 0 && strings.TrimSpace(stdout) == "" {
			return "Папка пуста."
		}
		return ""
	}

	return ""
}

// Format выводит результат в понятном формате.
func (r Result) Format() string {
	return r.Hint
}

