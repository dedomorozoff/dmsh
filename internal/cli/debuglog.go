package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dedomorozoff/dmsh/internal/redact"
)

type debugLogger struct {
	mu      sync.Mutex
	w       io.WriteCloser
	enabled bool
	secrets []string
}

func newDebugLogger(enabled bool, path string, secrets ...string) (*debugLogger, error) {
	if !enabled {
		return &debugLogger{enabled: false, secrets: secrets}, nil
	}
	if path == "" {
		path = "dmsh-debug.log"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	l := &debugLogger{w: f, enabled: true, secrets: secrets}
	l.printf("debug logging enabled")
	return l, nil
}

func (l *debugLogger) close() {
	if l == nil || !l.enabled || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.w.Close()
	l.w = nil
	l.enabled = false
}

func (l *debugLogger) printf(format string, args ...any) {
	if l == nil || !l.enabled || l.w == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	msg = redact.Text(msg, l.secrets...)
	line := fmt.Sprintf("%s %s\n", time.Now().Format(time.RFC3339Nano), msg)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w != nil {
		_, _ = io.WriteString(l.w, line)
	}
}

func (l *debugLogger) event(name string, kv ...any) {
	if l == nil || !l.enabled {
		return
	}
	msg := name
	if len(kv) > 0 {
		msg += " " + fmt.Sprint(kv...)
	}
	l.printf("%s", msg)
}
