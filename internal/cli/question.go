package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/chzyer/readline"
)

func readQuestionInput(in io.Reader, out io.Writer, promptStr string) (string, error) {
	isTTY := isTerminal(in)
	if isTTY {
		rl, rlErr := readline.NewEx(&readline.Config{
			Prompt: promptStr,
			Stdout: out,
			Stdin:  io.NopCloser(in),
		})
		if rlErr == nil {
			defer rl.Close()
			line, rlReadErr := rl.Readline()
			if rlReadErr != nil {
				return "", rlReadErr
			}
			return strings.TrimSpace(line), nil
		}
	}

	fmt.Fprint(out, promptStr)
	flushOutput(out)
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(sc.Text()), nil
}
