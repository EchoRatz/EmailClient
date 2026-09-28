package smtp_test

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"email-client/pkg/smtp"
)

// fakeServer accepts one connection and answers like a minimal SMTP server.
// rcptReply lets a test reject recipients. Every line the client sends is
// delivered on the returned channel once the session ends.
func fakeServer(t *testing.T, rcptReply string) (string, <-chan []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	got := make(chan []string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		send := func(s string) { io.WriteString(conn, s+"\r\n") }

		var lines []string
		defer func() { got <- lines }()

		send("220 fake ESMTP ready")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			lines = append(lines, line)

			if inData {
				if line == "." {
					inData = false
					send("250 OK queued")
				}
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				send("250-fake greets you")
				send("250 8BITMIME")
			case strings.HasPrefix(line, "MAIL FROM"):
				send("250 OK")
			case strings.HasPrefix(line, "RCPT TO"):
				send(rcptReply)
			case line == "DATA":
				inData = true
				send("354 End data with <CR><LF>.<CR><LF>")
			case line == "QUIT":
				send("221 bye")
				return
			default:
				send("500 unknown command")
			}
		}
	}()
	return ln.Addr().String(), got
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSendFullSession(t *testing.T) {
	addr, got := fakeServer(t, "250 OK")
	msg := []byte("Subject: Hi\n\nline one\n.starts with dot\n")

	err := smtp.Send(addr, 2*time.Second, quietLog(), "alice@x", []string{"bob@x", "carol@x"}, msg)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"EHLO email-client",
		"MAIL FROM:<alice@x>",
		"RCPT TO:<bob@x>",
		"RCPT TO:<carol@x>",
		"DATA",
		"Subject: Hi",
		"",
		"line one",
		"..starts with dot",
		".",
		"QUIT",
	}
	lines := <-got
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("client sent:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestSendRecipientRejected(t *testing.T) {
	addr, _ := fakeServer(t, "550 no such user")

	err := smtp.Send(addr, 2*time.Second, quietLog(), "alice@x", []string{"ghost@x"}, []byte("hi\n"))
	if err == nil || !strings.Contains(err.Error(), "RCPT TO ghost@x") {
		t.Fatalf("err = %v, want RCPT rejection", err)
	}
}

func TestConnectionRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	if _, err := smtp.Dial(addr, time.Second, quietLog()); err == nil {
		t.Fatal("expected connect error")
	}
}

func TestGreetingTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(2 * time.Second)
		}
	}()

	start := time.Now()
	_, err = smtp.Dial(ln.Addr().String(), 200*time.Millisecond, quietLog())
	var ne net.Error
	if err == nil || !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("err = %v, want timeout", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}
