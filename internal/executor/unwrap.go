package executor

import (
	"strings"
	"unicode"
)

// unwrapShellCommand removes a redundant shell wrapper from command when the
// command already invokes the same shell that prepareCommand will use.
// This prevents double-wrapping like:
//
//	powershell -Command "powershell -Command \"...\""
//
// which breaks quoting and can corrupt variables like $s.
func unwrapShellCommand(shell, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	kind := shellKind(shell)
	if kind == "" {
		return command
	}
	inner, ok := extractShellScript(command, kind)
	if !ok {
		return command
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return command
	}
	return inner
}

func shellKind(shell string) string {
	base := strings.ToLower(shell)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "powershell", "pwsh":
		return "powershell"
	case "cmd":
		return "cmd"
	case "sh", "bash", "zsh", "fish", "dash", "ksh":
		return "unix"
	default:
		return ""
	}
}

func extractShellScript(command, kind string) (string, bool) {
	tokens := splitCommandTokens(command)
	if len(tokens) < 2 {
		return "", false
	}
	cmdBase := strings.ToLower(tokens[0].text)
	if i := strings.LastIndexAny(cmdBase, `/\`); i >= 0 {
		cmdBase = cmdBase[i+1:]
	}
	cmdBase = strings.TrimSuffix(cmdBase, ".exe")

	switch kind {
	case "powershell":
		if cmdBase != "powershell" && cmdBase != "pwsh" {
			return "", false
		}
		for i := 1; i < len(tokens); i++ {
			if isPowerShellCommandFlag(tokens[i].text) {
				rest := strings.TrimSpace(command[tokens[i].end:])
				return stripOuterQuotes(rest), true
			}
		}
		return "", false
	case "cmd":
		if cmdBase != "cmd" {
			return "", false
		}
		for i := 1; i < len(tokens); i++ {
			flag := strings.ToLower(tokens[i].text)
			if flag == "/c" || flag == "/k" || flag == "-c" {
				rest := strings.TrimSpace(command[tokens[i].end:])
				return stripOuterQuotes(rest), true
			}
		}
		return "", false
	case "unix":
		if !isUnixShellName(cmdBase) {
			return "", false
		}
		for i := 1; i < len(tokens); i++ {
			if tokens[i].text == "-c" {
				rest := strings.TrimSpace(command[tokens[i].end:])
				return stripOuterQuotes(rest), true
			}
		}
		return "", false
	}
	return "", false
}

func isPowerShellCommandFlag(tok string) bool {
	switch strings.ToLower(tok) {
	case "-command", "-c", "/command", "/c":
		return true
	}
	return false
}

func isUnixShellName(base string) bool {
	switch base {
	case "sh", "bash", "zsh", "fish", "dash", "ksh":
		return true
	}
	return false
}

type cmdToken struct {
	text       string
	start, end int
}

// splitCommandTokens splits command into tokens with byte offsets, respecting
// simple single/double quoting and backslash escapes.
func splitCommandTokens(s string) []cmdToken {
	var tokens []cmdToken
	i := 0
	for i < len(s) {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		var b strings.Builder
		for i < len(s) {
			c := s[i]
			if unicode.IsSpace(rune(c)) {
				break
			}
			if c == '"' || c == '\'' {
				q := c
				i++ // consume opening quote
				for i < len(s) {
					if s[i] == '\\' && q == '"' && i+1 < len(s) {
						b.WriteByte(s[i])
						b.WriteByte(s[i+1])
						i += 2
						continue
					}
					if s[i] == q {
						i++ // consume closing quote
						break
					}
					b.WriteByte(s[i])
					i++
				}
				continue
			}
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i])
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			b.WriteByte(c)
			i++
		}
		end := i
		tokens = append(tokens, cmdToken{text: b.String(), start: start, end: end})
	}
	return tokens
}

func stripOuterQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
