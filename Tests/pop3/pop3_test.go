package pop3_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"email-client/pkg/pop3"
)

const secret = "s3cret-pw"

var mails = []string{
	"From: bob@x\r\nSubject: one\r\n\r\nfirst body\r\n",
	"From: carol@x\r\nSubject: two\r\n\r\n..leading dot\r\nlast line\r\n",
}

// fakeServer serves the mails above for any user with password secret.
// If cutRetr is true it closes the socket halfway through RETR.
func fakeServer(t *testing.T, cutRetr bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		send := func(s string) { io.WriteString(conn, s+"\r\n") }

		send("+OK fake POP3 ready")
		authed := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd, arg, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
			switch {
			case cmd == "USER":
				send("+OK")
			case cmd == "PASS" && arg == secret:
				authed = true
				send("+OK logged in")
			case cmd == "PASS":
				send("-ERR invalid credentials")
			case !authed:
				send("-ERR not authenticated")
			case cmd == "STAT":
				send(fmt.Sprintf("+OK %d %d", len(mails), len(mails[0])+len(mails[1])))
			case cmd == "LIST":
				send("+OK 2 messages")
				for i, m := range mails {
					send(fmt.Sprintf("%d %d", i+1, len(m)))
				}
				send(".")
			case cmd == "RETR":
				var n int
				fmt.Sscanf(arg, "%d", &n)
				if n < 1 || n > len(mails) {
					send("-ERR no such message")
					continue
				}
				send("+OK message follows")
				if cutRetr {
					io.WriteString(conn, mails[n-1][:10])
					return
				}
				io.WriteString(conn, mails[n-1]+".\r\n")
			case cmd == "QUIT":
				send("+OK bye")
				return
			default:
				send("-ERR unknown")
			}
		}
	}()
	return ln.Addr().String()
}

func dial(t *testing.T, addr string, log *slog.Logger) *pop3.Client {
	t.Helper()
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	c, err := pop3.Dial(addr, 2*time.Second, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSession(t *testing.T) {
	c := dial(t, fakeServer(t, false), nil)
	if err := c.Auth("alice", secret); err != nil {
		t.Fatal(err)
	}

	count, _, err := c.Stat()
	if err != nil || count != 2 {
		t.Fatalf("Stat = %d, %v", count, err)
	}

	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[1].Num != 2 || list[1].Size != len(mails[1]) {
		t.Errorf("List = %+v", list)
	}

	raw, err := c.Retr(2)
	if err != nil {
		t.Fatal(err)
	}
	want := "From: carol@x\nSubject: two\n\n.leading dot\nlast line\n"
	if string(raw) != want {
		t.Errorf("Retr = %q\nwant %q", raw, want)
	}

	var perr *pop3.Error
	if _, err := c.Retr(9); !errors.As(err, &perr) {
		t.Errorf("Retr(9) err = %v, want *pop3.Error", err)
	}

	if err := c.Quit(); err != nil {
		t.Error(err)
	}
}

func TestBadPassword(t *testing.T) {
	c := dial(t, fakeServer(t, false), nil)
	err := c.Auth("alice", "wrong")

	var perr *pop3.Error
	if !errors.As(err, &perr) || perr.Cmd != "PASS" {
		t.Fatalf("err = %v, want PASS *pop3.Error", err)
	}
}

func TestInterruptedTransfer(t *testing.T) {
	c := dial(t, fakeServer(t, true), nil)
	if err := c.Auth("alice", secret); err != nil {
		t.Fatal(err)
	}
	_, err := c.Retr(1)
	if err == nil || !strings.Contains(err.Error(), "transfer interrupted") {
		t.Fatalf("err = %v, want interrupted transfer", err)
	}
}

func TestPasswordNotLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := dial(t, fakeServer(t, false), log)
	c.Auth("alice", "wrong-"+secret)
	c.Auth("alice", secret)

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("password leaked:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "C: PASS ****") {
		t.Errorf("PASS command not traced:\n%s", buf.String())
	}
}
