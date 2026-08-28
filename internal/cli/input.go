package cli

import (
	"bufio"
	"errors"
	"io"
)

var ErrInterrupted = errors.New("interrupted")

var ErrSlashCommand = errors.New("slash command handled")

type LineReader interface {
	ReadLine() (string, error)
}

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

func NewBufioReader(in io.Reader) LineReader {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &bufioReader{sc: sc}
}
