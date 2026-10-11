package perf_test

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"email-client/pkg/account"
	"email-client/pkg/config"
)

// Run with: PERF=1 go test ./perf -v
// Uses the bob@localhost account so alice's inbox stays clean for demos.
// Writes the table to ../../Results/timing.md.

const runs = 10

var sizes = []struct {
	label string
	bytes int
}{
	{"1 KB", 1 << 10},
	{"10 KB", 10 << 10},
	{"100 KB", 100 << 10},
}

// body builds plain text of about n bytes in 72-character lines.
func body(n int) string {
	line := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 2)[:71] + "\n"
	return strings.Repeat(line, n/len(line)+1)[:n]
}

type stats struct{ min, median, mean, max float64 }

func summarize(d []time.Duration) stats {
	ms := make([]float64, len(d))
	sum := 0.0
	for i, x := range d {
		ms[i] = float64(x.Microseconds()) / 1000
		sum += ms[i]
	}
	slices.Sort(ms)
	return stats{ms[0], ms[len(ms)/2], sum / float64(len(ms)), ms[len(ms)-1]}
}

func row(op, size string, s stats) string {
	return fmt.Sprintf("| %s | %s | %.1f | %.1f | %.1f | %.1f |\n", op, size, s.min, s.median, s.mean, s.max)
}

func timeIt(t *testing.T, fn func() error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := fn(); err != nil {
		t.Fatal(err)
	}
	return time.Since(start)
}

func TestTiming(t *testing.T) {
	if os.Getenv("PERF") == "" {
		t.Skip("set PERF=1 to run the timing measurements")
	}
	cfg, err := config.Load("../../Client/config.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Username, cfg.Password, cfg.From = "bob@localhost", "bob", "bob@localhost"
	cfg.MailboxPath = filepath.Join(t.TempDir(), "mailbox.json")
	acct, err := account.Open(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	tag := fmt.Sprintf("perf-%d", time.Now().Unix())
	var table strings.Builder

	// Send: one full SMTP session per message (connect, EHLO, MAIL, RCPT, DATA, QUIT).
	for _, sz := range sizes {
		text := body(sz.bytes)
		var d []time.Duration
		for i := range runs {
			subject := fmt.Sprintf("%s %s #%d", tag, sz.label, i)
			d = append(d, timeIt(t, func() error { return acct.Send([]string{"bob@localhost"}, subject, text) }))
		}
		table.WriteString(row("Send (SMTP session)", sz.label, summarize(d)))
	}

	// Find one message of each size by its subject.
	items, _, err := acct.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	num := map[string]int{}
	for _, it := range items {
		for _, sz := range sizes {
			if it.Subject == fmt.Sprintf("%s %s #0", tag, sz.label) {
				num[sz.label] = it.Num
			}
		}
	}

	// Retrieve one: connect, USER, PASS, RETR n, QUIT.
	for _, sz := range sizes {
		n, ok := num[sz.label]
		if !ok {
			t.Fatalf("sent %s message not found on server", sz.label)
		}
		var d []time.Duration
		for range runs {
			d = append(d, timeIt(t, func() error { _, err := acct.Fetch(n); return err }))
		}
		table.WriteString(row("Retrieve one (POP3 RETR)", sz.label, summarize(d)))
	}

	// Refresh: LIST + RETR of every message in the inbox.
	var d []time.Duration
	for range 5 {
		d = append(d, timeIt(t, func() error { _, _, err := acct.Refresh(); return err }))
	}
	table.WriteString(row(fmt.Sprintf("Check inbox (LIST + RETR all %d)", len(items)), "mixed", summarize(d)))

	out := fmt.Sprintf(`# Timing

Measured %s · %s %s/%s · GreenMail in Docker on localhost · %d runs per row (inbox check: 5).
Each time covers a whole protocol session including the TCP connect and QUIT.

| Operation | Message size | Min (ms) | Median (ms) | Mean (ms) | Max (ms) |
|---|---|---:|---:|---:|---:|
%s`, time.Now().Format("2006-01-02 15:04"), runtime.Version(), runtime.GOOS, runtime.GOARCH, runs, table.String())

	t.Log("\n" + out)
	if err := os.MkdirAll("../../Results", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../../Results/timing.md", []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
