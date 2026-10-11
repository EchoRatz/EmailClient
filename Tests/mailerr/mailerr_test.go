package mailerr_test

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"email-client/pkg/mailerr"
	"email-client/pkg/message"
	"email-client/pkg/pop3"
	"email-client/pkg/smtp"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// listen starts a one-connection server that runs script on the accepted socket.
func listen(t *testing.T, script func(net.Conn, *bufio.Reader)) string {
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
		script(conn, bufio.NewReader(conn))
	}()
	return ln.Addr().String()
}

func closedPort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func say(c net.Conn, lines ...string) {
	for _, l := range lines {
		io.WriteString(c, l+"\r\n")
	}
}

func popLogin(t *testing.T, addr, pass string) error {
	c, err := pop3.Dial(addr, time.Second, quiet)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Auth("alice", pass)
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		want mailerr.Kind
		run  func(t *testing.T) error
	}{
		{"connection refused", mailerr.Unreachable, func(t *testing.T) error {
			_, err := smtp.Dial(closedPort(t), time.Second, quiet)
			return err
		}},
		{"unknown host", mailerr.Unreachable, func(t *testing.T) error {
			_, err := pop3.Dial("no-such-host.invalid:110", time.Second, quiet)
			return err
		}},
		{"silent server", mailerr.Timeout, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, _ *bufio.Reader) { time.Sleep(time.Second) })
			_, err := smtp.Dial(addr, 200*time.Millisecond, quiet)
			return err
		}},
		{"wrong password", mailerr.AuthFailed, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, r *bufio.Reader) {
				say(c, "+OK ready")
				r.ReadString('\n')
				say(c, "+OK")
				r.ReadString('\n')
				say(c, "-ERR invalid password")
			})
			return popLogin(t, addr, "wrong")
		}},
		{"recipient rejected", mailerr.Rejected, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, r *bufio.Reader) {
				say(c, "220 ready")
				r.ReadString('\n')
				say(c, "250 hi")
				r.ReadString('\n')
				say(c, "250 ok")
				r.ReadString('\n')
				say(c, "550 no such user")
			})
			return smtp.Send(addr, time.Second, quiet, "a@x", []string{"ghost@x"}, []byte("hi\n"))
		}},
		{"not a POP3 server", mailerr.Unexpected, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, _ *bufio.Reader) { say(c, "HTTP/1.1 400 Bad Request") })
			_, err := pop3.Dial(addr, time.Second, quiet)
			return err
		}},
		{"not an SMTP server", mailerr.Unexpected, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, _ *bufio.Reader) { say(c, "hello there") })
			_, err := smtp.Dial(addr, time.Second, quiet)
			return err
		}},
		{"dropped mid-RETR", mailerr.Interrupted, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, r *bufio.Reader) {
				say(c, "+OK ready")
				r.ReadString('\n')
				say(c, "+OK message follows", "Subject: half")
			})
			c, err := pop3.Dial(addr, time.Second, quiet)
			if err != nil {
				return err
			}
			defer c.Close()
			_, err = c.Retr(1)
			return err
		}},
		{"dropped after greeting", mailerr.Interrupted, func(t *testing.T) error {
			addr := listen(t, func(c net.Conn, r *bufio.Reader) {
				say(c, "220 ready")
				r.ReadString('\n')
			})
			return smtp.Send(addr, time.Second, quiet, "a@x", []string{"b@x"}, []byte("hi\n"))
		}},
		{"bad recipient input", mailerr.InvalidInput, func(t *testing.T) error {
			_, err := message.ParseRecipients("not-an-address")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			if got := mailerr.Classify(err); got != tc.want {
				t.Errorf("Classify(%v) = %q, want %q", err, got, tc.want)
			}
			_, headline, _ := mailerr.Explain(err)
			if strings.TrimSpace(headline) == "" {
				t.Error("empty headline")
			}
		})
	}
}

func TestInputHeadlineIsTheMessage(t *testing.T) {
	_, headline, detail := mailerr.Explain(mailerr.Input("bad thing %d", 7))
	if headline != "bad thing 7" || detail != "" {
		t.Errorf("got %q / %q", headline, detail)
	}
}
