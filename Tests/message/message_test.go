package message_test

import (
	"strings"
	"testing"
	"time"

	"email-client/pkg/message"
)

func TestBuildThenParse(t *testing.T) {
	date := time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC)
	raw := message.Build("alice@x", []string{"bob@x", "carol@x"}, "Café menu", "hello\r\nworld", date)

	m, err := message.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "alice@x" || m.To != "bob@x, carol@x" {
		t.Errorf("From/To = %q / %q", m.From, m.To)
	}
	if m.Subject != "Café menu" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if !m.Date.Equal(date) {
		t.Errorf("Date = %v, want %v", m.Date, date)
	}
	if m.Body != "hello\nworld\n" {
		t.Errorf("Body = %q", m.Body)
	}
}

func TestSubjectCannotInjectHeaders(t *testing.T) {
	raw := message.Build("a@x", []string{"b@x"}, "hi\nBcc: evil@x", "body", time.Now())
	if strings.Contains(string(raw), "\nBcc:") {
		t.Fatalf("newline in subject produced a header:\n%s", raw)
	}
}

func TestParseForeignMessage(t *testing.T) {
	raw := "From: Bob <bob@x>\r\nSubject: =?utf-8?q?caf=C3=A9?=\r\n\r\nbody text\r\n"
	m, err := message.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "Bob <bob@x>" || m.Subject != "café" || !m.Date.IsZero() {
		t.Errorf("got %+v", m)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := message.Parse([]byte("no header here")); err == nil {
		t.Fatal("expected error")
	}
}
