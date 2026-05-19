package cli

import (
	"bufio"
	"errors"
	"io"

	"github.com/chzyer/readline"
)

// ErrInterrupted is returned when input is interrupted (Ctrl+C).
var ErrInterrupted = errors.New("interrupted")

// ErrSlashCommand is returned when a slash command was handled during follow-up.
var ErrSlashCommand = errors.New("slash command handled")

// LineReader abstracts line input for both readline and fallback modes.
type LineReader interface {
	ReadLine() (string, error)
}

// readlineReader wraps *readline.Instance to implement LineReader.
type readlineReader struct {
	rl *readline.Instance
}

func (r *readlineReader) ReadLine() (string, error) {
	line, err := r.rl.Readline()
	if err != nil {
		if errors.Is(err, readline.ErrInterrupt) {
			return "", ErrInterrupted
		}
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return "", err
	}
	return line, nil
}

// bufioReader wraps bufio.Scanner to implement LineReader.
type bufioReader struct {
	sc *bufio.Scanner
}

func (r *bufioReader) ReadLine() (string, error) {
	if !r.sc.Scan() {
		if err := r.sc.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return r.sc.Text(), nil
}

// NewReadlineReader creates a LineReader from *readline.Instance.
func NewReadlineReader(rl *readline.Instance) LineReader {
	return &readlineReader{rl: rl}
}

// NewBufioReader creates a LineReader from io.Reader.
func NewBufioReader(in io.Reader) LineReader {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &bufioReader{sc: sc}
}
