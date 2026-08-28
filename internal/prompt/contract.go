package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Intent перечисляет типы намерений, которые модель может вернуть.
type Intent string

const (
	IntentRunCommand       Intent = "run_command"
	IntentExplain          Intent = "explain"
	IntentAskClarification Intent = "ask_clarification"
)

// Risk — категория риска предлагаемой команды.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Response — это контракт, по которому LLM обязана возвращать ответ.
// Мы парсим строго это и ничего больше; всё остальное — ошибка формата.
type Response struct {
	Intent            Intent `json:"intent"`
	Command           string `json:"command,omitempty"`
	Explanation       string `json:"explanation,omitempty"`
	Risk              Risk   `json:"risk_level,omitempty"`
	NeedsConfirmation bool   `json:"needs_confirmation,omitempty"`
	Question          string `json:"question,omitempty"`
}

// Validate проверяет внутреннюю согласованность ответа и нормализует поля.
func (r *Response) Validate() error {
	// Some models omit "intent" despite the schema. Infer it from the fields
	// so the response is still actionable instead of being rejected.
	if r.Intent == "" {
		switch {
		case strings.TrimSpace(r.Command) != "":
			r.Intent = IntentRunCommand
		case strings.TrimSpace(r.Question) != "":
			r.Intent = IntentAskClarification
		case strings.TrimSpace(r.Explanation) != "":
			r.Intent = IntentExplain
		}
	}
	switch r.Intent {
	case IntentRunCommand:
		if strings.TrimSpace(r.Command) == "" {
			return errors.New("intent=run_command, but command is empty")
		}
		if r.Risk == "" {
			r.Risk = RiskMedium
		}
		if r.Risk == RiskMedium || r.Risk == RiskHigh {
			r.NeedsConfirmation = true
		}
	case IntentExplain:
		if strings.TrimSpace(r.Explanation) == "" {
			return errors.New("intent=explain, but explanation is empty")
		}
	case IntentAskClarification:
		if strings.TrimSpace(r.Question) == "" && strings.TrimSpace(r.Explanation) == "" {
			return errors.New("intent=ask_clarification, but question and explanation are empty")
		}
	default:
		return fmt.Errorf("unknown intent: %q", r.Intent)
	}
	return nil
}

// Parse извлекает первый JSON-объект из произвольного текста модели и
// разбирает его в Response. Толерантен к префиксам/суффиксам типа
// "Sure, here is the JSON:" и тройных бэктиков.
func Parse(raw string) (Response, error) {
	objects := extractAllJSONObjects(raw)
	if len(objects) == 0 {
		if fallbackResp, err := parseFallback(raw); err == nil {
			return fallbackResp, nil
		}
		return Response{}, fmt.Errorf("no JSON object found in model output")
	}

	var lastErr error
	for _, jsonText := range objects {
		// Удаляем markdown-обёртки ```json ... ```
		cleaned := strings.TrimSpace(jsonText)
		if strings.HasPrefix(cleaned, "```json") {
			cleaned = strings.TrimPrefix(cleaned, "```json")
			cleaned = strings.TrimSuffix(cleaned, "```")
			cleaned = strings.TrimSpace(cleaned)
		} else if strings.HasPrefix(cleaned, "```") {
			cleaned = strings.TrimPrefix(cleaned, "```")
			cleaned = strings.TrimSuffix(cleaned, "```")
			cleaned = strings.TrimSpace(cleaned)
		}

		var resp Response
		dec := json.NewDecoder(strings.NewReader(cleaned))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&resp); err != nil {
			if err2 := json.Unmarshal([]byte(cleaned), &resp); err2 != nil {
				lastErr = fmt.Errorf("decode json: %w", err)
				continue
			}
		}
		if err := resp.Validate(); err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}

	if fallbackResp, err := parseFallback(raw); err == nil {
		return fallbackResp, nil
	}

	return Response{}, fmt.Errorf("invalid response: %w", lastErr)
}

func parseFallback(raw string) (Response, error) {
	var resp Response

	// Extract intent
	reIntent := regexp.MustCompile(`"intent"\s*:\s*"([^"]+)"`)
	if match := reIntent.FindStringSubmatch(raw); len(match) > 1 {
		resp.Intent = Intent(match[1])
	}

	// Extract command (handling escaped quotes inside)
	reCommand := regexp.MustCompile(`"command"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	if match := reCommand.FindStringSubmatch(raw); len(match) > 1 {
		resp.Command = unescapeString(match[1])
	}

	// Extract explanation
	reExplanation := regexp.MustCompile(`"explanation"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	if match := reExplanation.FindStringSubmatch(raw); len(match) > 1 {
		resp.Explanation = unescapeString(match[1])
	}

	// Extract question
	reQuestion := regexp.MustCompile(`"question"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	if match := reQuestion.FindStringSubmatch(raw); len(match) > 1 {
		resp.Question = unescapeString(match[1])
	}

	// Extract risk_level
	reRisk := regexp.MustCompile(`"risk_level"\s*:\s*"([^"]+)"`)
	if match := reRisk.FindStringSubmatch(raw); len(match) > 1 {
		resp.Risk = Risk(match[1])
	}

	// Extract needs_confirmation
	reConfirm := regexp.MustCompile(`"needs_confirmation"\s*:\s*(true|false)`)
	if match := reConfirm.FindStringSubmatch(raw); len(match) > 1 {
		resp.NeedsConfirmation = match[1] == "true"
	}

	if err := resp.Validate(); err != nil {
		return Response{}, fmt.Errorf("fallback validation failed: %w", err)
	}

	return resp, nil
}

func unescapeString(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

// extractAllJSONObjects извлекает все сбалансированные JSON-объекты из строки.
// Обрабатывает некорректно экранированные кавычки внутри строк (частая проблема LLM).
func extractAllJSONObjects(s string) []string {
	var results []string
	for {
		start := strings.IndexByte(s, '{')
		if start == -1 {
			break
		}
		s = s[start:]
		depth := 0
		inString := false
		escaped := false
		end := -1
	loop:
		for i := 0; i < len(s); i++ {
			c := s[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == '"' {
					inString = false
				}
				continue
			}
			switch c {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i + 1
					break loop
				}
			}
		}
		if end == -1 {
			break
		}
		jsonObj := s[:end]
		// Исправляем некорректно экранированные кавычки внутри строк
		jsonObj = fixUnescapedQuotes(jsonObj)
		results = append(results, jsonObj)
		s = s[end:]
		// Пропускаем разделители между JSON-объектами
		s = strings.TrimLeft(s, " \t\r\n-")
	}
	return results
}

// fixUnescapedQuotes исправляет некорректно экранированные кавычки внутри JSON-строк.
// Модели часто генерируют: "command": "echo "hello"" вместо "command": "echo \"hello\""
func fixUnescapedQuotes(jsonStr string) string {
	var result strings.Builder
	result.Grow(len(jsonStr) + 64)

	inString := false
	escaped := false

	for i := 0; i < len(jsonStr); i++ {
		c := jsonStr[i]

		if inString {
			if escaped {
				escaped = false
				result.WriteByte(c)
				continue
			}
			if c == '\\' {
				escaped = true
				result.WriteByte(c)
				continue
			}
			if c == '"' {
				// Проверяем, является ли это концом строки или вложенной кавычкой
				// Если после кавычки идёт :, }, ], ,, то это конец строки
				rest := strings.TrimLeft(jsonStr[i+1:], " \t\r\n")
				if len(rest) > 0 {
					nextChar := rest[0]
					if nextChar == ':' || nextChar == '}' || nextChar == ']' || nextChar == ',' {
						// Это закрывающая кавычка поля
						inString = false
						result.WriteByte(c)
						continue
					}
				} else {
					// Конец строки - закрывающая кавычка
					inString = false
					result.WriteByte(c)
					continue
				}
				// Это вложенная кавычка - экранируем её
				result.WriteString("\\\"")
				continue
			}
			result.WriteByte(c)
			continue
		}

		if c == '"' {
			inString = true
		}
		result.WriteByte(c)
	}

	return result.String()
}
