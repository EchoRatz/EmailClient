package logger_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"email-client/pkg/logger"
)

func TestPasswordNeverLogged(t *testing.T) {
	const pw = "hunter2-very-secret"
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelDebug)

	log.Info("login", "user", "alice", "password", pw)
	log.Info("login", "PASS", pw)
	log.With("password", pw).Error("auth failed")
	log.Info("nested", slog.Group("pop3", "password", pw))

	out := buf.String()
	if strings.Contains(out, pw) {
		t.Fatalf("password leaked into log:\n%s", out)
	}
	if n := strings.Count(out, "[REDACTED]"); n != 4 {
		t.Errorf("found %d redactions, want 4:\n%s", n, out)
	}
	if !strings.Contains(out, "user=alice") {
		t.Errorf("non-secret attr missing:\n%s", out)
	}
}
