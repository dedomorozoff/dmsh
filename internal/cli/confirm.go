package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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
		return false, nil
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
