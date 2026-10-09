package cli

import (
	"fmt"
	"strings"
)

// maxClarifyRounds ограничивает уточняющие вопросы подряд. Двух кругов
// хватает, чтобы разобраться с неоднозначностью; без предела модель
// способна переспрашивать одно и то же бесконечно.
const maxClarifyRounds = 2

// clarifyTurn — состоявшийся обмен «вопрос → ответ».
type clarifyTurn struct {
	question string
	answer   string
}

// clarification ведёт диалог уточнений текущего запроса. Накопленные пары
// уходят в следующий запрос к модели: без них модель не знает, что вопрос
// уже задан, и спрашивает его снова.
type clarification struct {
	round int
	turns []clarifyTurn
}

// followUp добавляет пару «вопрос → ответ» и строит текст следующего
// запроса: исходный запрос, все состоявшиеся пары и запрет повторяться.
func (c *clarification) followUp(input, question, answer string) string {
	repeated := c.repeatsLast(question)
	c.round++
	c.turns = append(c.turns, clarifyTurn{question: question, answer: answer})

	var b strings.Builder
	b.WriteString(input)
	for i, t := range c.turns {
		fmt.Fprintf(&b, "\n\n[clarification %d/%d]\nYou already asked: %s\nThe user answered: %s",
			i+1, maxClarifyRounds, t.question, t.answer)
	}
	b.WriteString("\n")
	switch {
	case c.round >= maxClarifyRounds:
		b.WriteString("That was your last clarification. Do not ask anything else: " +
			"continue with the best assumption implied by the answers above.")
	case repeated:
		b.WriteString("You just repeated a question the user already answered. Use that answer, or make a different choice yourself.")
	default:
		b.WriteString("Do not ask a question you have already asked. If you still need " +
			"clarification, ask something different, otherwise continue with the answer.")
	}
	return b.String()
}

// exhausted сообщает, что круги уточнений закончились.
func (c *clarification) exhausted() bool {
	return c.round >= maxClarifyRounds
}

// repeatsLast сообщает, что модель дословно повторила предыдущий вопрос:
// такой ответ ничего не уточняет, и спрашивать дальше бессмысленно.
func (c *clarification) repeatsLast(question string) bool {
	if len(c.turns) == 0 {
		return false
	}
	q := strings.TrimSpace(question)
	return q != "" && strings.EqualFold(q, strings.TrimSpace(c.turns[len(c.turns)-1].question))
}
