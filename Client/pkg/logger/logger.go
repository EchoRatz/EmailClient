package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const redacted = "[REDACTED]"

var secretKeys = map[string]bool{
	"password": true,
	"pass":     true,
	"secret":   true,
}

func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redact,
	}))
}

// Open appends to the log file at path, creating its directory if needed.
func Open(path string, level slog.Level) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return New(f, level), f, nil
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if secretKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, redacted)
	}
	return a
}
