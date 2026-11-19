package policy

import "strings"

// maxNestedDepth ограничивает рекурсию по вложенным оболочкам. Глубже
// четырёх уровней (`sudo bash -c "sh -c '...'"`) команда уже нечитаема;
// дальше считаем её подозрительной, но не зацикливаемся.
const maxNestedDepth = 4

// shellTok — слово команды или разделитель (`;`, `|`, `&&`, скобки).
// Разделители нужны, чтобы отличить команду в начале строки от простого
// упоминания: `echo bash -c 'rm -rf /'` ничего не выполняет.
type shellTok struct {
	text string
	sep  bool
}

// nestedScripts возвращает тексты, которые оболочка выполнит внутри
// обёртки: `bash -c`, `cmd /c`, `powershell -Command`, `eval`, `su -c`,
// интерпретаторы (`python -c`, `node -e`) и `find -exec`.
//
// Без этого кавычки обходили denylist: `bash -c "rm -rf /"` не совпадал
// с шаблоном `rm -rf /`, потому что после слэша шла кавычка, а не конец
// строки.
func nestedScripts(cmd string) []string {
	toks := tokenizeShell(cmd)
	var out []string

	cmdStart := true
	// prefix — только что прочитана обёртка-префикс (sudo, env, xargs):
	// следующие флаги, числа и присваивания относятся к ней, а оболочка
	// всё ещё может стоять в начале команды.
	prefix := false

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.sep || t.text == "-exec" || t.text == "-execdir" {
			cmdStart, prefix = true, false
			continue
		}

		word := t.text
		if cmdStart || prefix {
			switch {
			case isAssignment(word), isNumber(word):
				continue
			case prefix && strings.HasPrefix(word, "-"):
				// Флаг префикса: `sudo -E`, `nice -n`, `xargs -0`.
				continue
			case isPrefixWord(word):
				prefix = true
				continue
			}

			base := shellBase(word)
			if script, next, ok := scriptAfter(toks, i, base); ok {
				if strings.TrimSpace(script) != "" {
					out = append(out, script)
				}
				i = next
			}
			cmdStart, prefix = false, false
			continue
		}
	}

	return out
}

// scriptAfter ищет аргумент-скрипт у обёртки base, начиная с позиции from.
// Возвращает сам скрипт и позицию его последнего токена.
func scriptAfter(toks []shellTok, from int, base string) (string, int, bool) {
	if base == "eval" {
		parts := make([]string, 0, 4)
		last := from
		for j := from + 1; j < len(toks); j++ {
			if toks[j].sep {
				break
			}
			parts = append(parts, toks[j].text)
			last = j
		}
		if len(parts) == 0 {
			return "", from, false
		}
		return strings.Join(parts, " "), last, true
	}
	if !isWrapper(base) {
		return "", from, false
	}

	for j := from + 1; j < len(toks); j++ {
		if toks[j].sep {
			return "", from, false
		}
		w := toks[j].text
		if !scriptFlag(base, w) {
			// У этих обёрток между флагами бывают значения (`-NoProfile`,
			// `/q`, имя пользователя у su), поэтому идём дальше, пока не
			// встретим флаг команды.
			switch base {
			case "powershell", "pwsh", "cmd", "su", "runuser":
				continue
			}
			return "", from, false
		}
		if j+1 >= len(toks) || toks[j+1].sep {
			return "", from, false
		}
		return toks[j+1].text, j + 1, true
	}
	return "", from, false
}

// isWrapper — программы, которые исполняют переданную им строку как код.
func isWrapper(base string) bool {
	switch base {
	case "sh", "bash", "zsh", "dash", "ksh", "ash", "fish",
		"cmd", "powershell", "pwsh",
		"su", "runuser",
		"python", "python2", "python3", "node", "nodejs",
		"perl", "ruby", "php", "osascript":
		return true
	}
	return false
}

// scriptFlag сообщает, каким флагом обёртка получает текст команды.
func scriptFlag(base, arg string) bool {
	lower := strings.ToLower(arg)
	switch base {
	case "sh", "bash", "zsh", "dash", "ksh", "ash", "fish":
		// -c, -lc, -ic: буква c в любом месте короткого флага.
		return len(lower) >= 2 && lower[0] == '-' && lower[1] != '-' &&
			strings.ContainsRune(lower[1:], 'c')
	case "cmd":
		return lower == "/c" || lower == "/k"
	case "powershell", "pwsh":
		return lower == "-c" || lower == "-command" ||
			strings.HasPrefix(lower, "-enc") || lower == "-ec"
	case "su", "runuser":
		return lower == "-c" || lower == "--command"
	case "python", "python2", "python3", "node", "nodejs", "perl", "ruby":
		return lower == "-c" || lower == "-e" || lower == "--eval"
	case "php":
		return lower == "-r"
	case "osascript":
		return lower == "-e"
	}
	return false
}

// isPrefixWord — команды, после которых исполняемая программа всё ещё
// находится в позиции команды: `sudo bash`, `env FOO=1 sh`, `xargs sh`.
func isPrefixWord(word string) bool {
	switch shellBase(word) {
	case "sudo", "doas", "env", "nice", "nohup", "time", "command",
		"exec", "stdbuf", "setsid", "xargs", "timeout", "watch":
		return true
	}
	return false
}

// shellBase приводит имя программы к нижнему регистру и отбрасывает путь
// и расширение: `/bin/BASH`, `C:\Windows\System32\cmd.exe` → `bash`, `cmd`.
func shellBase(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		w = w[i+1:]
	}
	return strings.TrimSuffix(w, ".exe")
}

func isAssignment(word string) bool {
	i := strings.IndexByte(word, '=')
	if i <= 0 {
		return false
	}
	for j, r := range word[:i] {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if j > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func isNumber(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// tokenizeShell режет строку на слова и разделители, не трогая содержимое
// кавычек: `bash -c "rm -rf /"` даёт слова `bash`, `-c`, `rm -rf /`.
// Обратный слэш не считается escape-символом, иначе виндовые пути
// (`C:\Users`) превращались бы в мусор.
func tokenizeShell(s string) []shellTok {
	var toks []shellTok
	var b strings.Builder
	var quote byte

	flush := func() {
		if b.Len() > 0 {
			toks = append(toks, shellTok{text: b.String()})
			b.Reset()
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '|', ';', '&', '(', ')', '<', '>', '\n':
			flush()
			toks = append(toks, shellTok{sep: true})
		case ' ', '\t', '\r':
			flush()
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return toks
}
