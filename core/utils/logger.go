package utils

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Logger is a minimal thread-safe logger used by the runtime.
type Logger struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewLogger creates a logger that writes to stdout.
func NewLogger() *Logger {
	return &Logger{writer: os.Stdout}
}

// Log prints a scoped log line in a predictable runtime format.
func (l *Logger) Log(scope, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintf(l.writer, "[%s] %s\n", scope, message)
}

// Writer exposes the underlying output stream.
func (l *Logger) Writer() io.Writer {
	return l.writer
}
