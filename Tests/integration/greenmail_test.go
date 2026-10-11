package integration_test

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"email-client/pkg/config"
	"email-client/pkg/mailbox"
	"email-client/pkg/message"
	"email-client/pkg/pop3"
	"email-client/pkg/smtp"
)

// These tests talk to the GreenMail container and are skipped when it is not running.

func setup(t *testing.T) (*config.Config, *slog.Logger) {
	t.Helper()
	cfg, err := config.Load("../../Client/config.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{cfg.SMTPAddr(), cfg.POP3Addr()} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Skipf("GreenMail not reachable at %s: %v", addr, err)
		}
		conn.Close()
	}
	return cfg, slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSendRetrieveSave(t *testing.T) {
	cfg, log := setup(t)
	tag := fmt.Sprintf("run-%d", time.Now().UnixNano())

	for i := 1; i <= 3; i++ {
		subject := fmt.Sprintf("%s #%d", tag, i)
		body := fmt.Sprintf("Message %d.\n.line starting with a dot\n", i)
		raw := message.Build(cfg.From, []string{cfg.From}, subject, body, time.Now())
		if err := smtp.Send(cfg.SMTPAddr(), cfg.Timeout(), log, cfg.From, []string{cfg.From}, raw); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	c, err := pop3.Dial(cfg.POP3Addr(), cfg.Timeout(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Auth(cfg.Username, cfg.Password); err != nil {
		t.Fatal(err)
	}
	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}

	mbPath := filepath.Join(t.TempDir(), "mailbox.json")
	mb, err := mailbox.Open(mbPath)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, m := range list {
		raw, err := c.Retr(m.Num)
		if err != nil {
			t.Fatalf("RETR %d: %v", m.Num, err)
		}
		e, _, err := mb.Add(raw, m.Num)
		if err != nil {
			t.Fatalf("parse %d: %v", m.Num, err)
		}
		if strings.HasPrefix(e.Subject, tag) {
			found++
			if !strings.Contains(e.Body, "\n.line starting with a dot\n") {
				t.Errorf("dot line damaged in %q: %q", e.Subject, e.Body)
			}
			if e.From != cfg.From || e.Date.IsZero() {
				t.Errorf("headers: from=%q date=%v", e.From, e.Date)
			}
		}
	}
	if err := c.Quit(); err != nil {
		t.Fatal(err)
	}
	if found != 3 {
		t.Fatalf("found %d of 3 sent messages in LIST of %d", found, len(list))
	}

	if err := mb.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := mailbox.Open(mbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.List()) != len(list) {
		t.Errorf("reopened mailbox has %d entries, want %d", len(again.List()), len(list))
	}
}

func TestWrongPassword(t *testing.T) {
	cfg, log := setup(t)
	c, err := pop3.Dial(cfg.POP3Addr(), cfg.Timeout(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var perr *pop3.Error
	if err := c.Auth(cfg.Username, "definitely-wrong"); !errors.As(err, &perr) {
		t.Fatalf("err = %v, want *pop3.Error", err)
	}
}
