package pop3

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"time"
)

// Error is a -ERR reply from the server.
type Error struct {
	Cmd string
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("pop3: %s: server replied -ERR %s", e.Cmd, e.Msg)
}

type MsgInfo struct {
	Num  int
	Size int
}

type Client struct {
	conn    net.Conn
	tp      *textproto.Conn
	log     *slog.Logger
	timeout time.Duration
}

func Dial(addr string, timeout time.Duration, log *slog.Logger) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("pop3: connect %s: %w", addr, err)
	}
	c := &Client{conn: conn, tp: textproto.NewConn(conn), log: log, timeout: timeout}

	if _, err := c.reply("greeting"); err != nil {
		conn.Close()
		return nil, err
	}
	c.log.Info("Connected to POP3 server", "addr", addr)
	return c, nil
}

func (c *Client) extend() {
	c.conn.SetDeadline(time.Now().Add(c.timeout))
}

// reply reads one status line and splits "+OK rest" / "-ERR rest".
func (c *Client) reply(cmd string) (string, error) {
	c.extend()
	line, err := c.tp.ReadLine()
	if err != nil {
		return "", fmt.Errorf("pop3: %s: %w", cmd, err)
	}
	c.log.Debug("S: " + line)

	status, rest, _ := strings.Cut(line, " ")
	switch status {
	case "+OK":
		return rest, nil
	case "-ERR":
		return "", &Error{Cmd: cmd, Msg: rest}
	default:
		return "", fmt.Errorf("pop3: %s: unexpected response %q", cmd, line)
	}
}

// cmd sends one command line and reads its status reply.
// The PASS argument is masked before the line reaches the log.
func (c *Client) cmd(line string) (string, error) {
	name, _, _ := strings.Cut(line, " ")
	shown := line
	if name == "PASS" {
		shown = "PASS ****"
	}
	c.extend()
	c.log.Debug("C: " + shown)
	if err := c.tp.PrintfLine("%s", line); err != nil {
		return "", fmt.Errorf("pop3: %s: %w", name, err)
	}
	return c.reply(name)
}

func (c *Client) Auth(user, pass string) error {
	if _, err := c.cmd("USER " + user); err != nil {
		c.log.Error("POP3 authentication failed", "user", user, "err", err)
		return err
	}
	if _, err := c.cmd("PASS " + pass); err != nil {
		c.log.Error("POP3 authentication failed", "user", user, "err", err)
		return err
	}
	c.log.Info("POP3 authenticated", "user", user)
	return nil
}

func (c *Client) Stat() (count, size int, err error) {
	rest, err := c.cmd("STAT")
	if err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(rest, "%d %d", &count, &size); err != nil {
		return 0, 0, fmt.Errorf("pop3: STAT: bad reply %q", rest)
	}
	return count, size, nil
}

func (c *Client) List() ([]MsgInfo, error) {
	if _, err := c.cmd("LIST"); err != nil {
		return nil, err
	}
	lines, err := c.tp.ReadDotLines()
	if err != nil {
		return nil, fmt.Errorf("pop3: LIST: %w", err)
	}

	msgs := make([]MsgInfo, 0, len(lines))
	for _, l := range lines {
		var m MsgInfo
		if _, err := fmt.Sscanf(l, "%d %d", &m.Num, &m.Size); err != nil {
			return nil, fmt.Errorf("pop3: LIST: bad line %q", l)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// Retr returns the raw message with dot-stuffing removed and CRLF turned into LF.
func (c *Client) Retr(n int) ([]byte, error) {
	if _, err := c.cmd(fmt.Sprintf("RETR %d", n)); err != nil {
		return nil, err
	}
	c.extend()
	raw, err := io.ReadAll(c.tp.DotReader())
	if err != nil {
		return nil, fmt.Errorf("pop3: RETR %d: transfer interrupted: %w", n, err)
	}
	c.log.Info(fmt.Sprintf("Retrieved message %d", n), "bytes", len(raw))
	return raw, nil
}

func (c *Client) Quit() error {
	_, err := c.cmd("QUIT")
	c.conn.Close()
	return err
}

func (c *Client) Close() error {
	return c.conn.Close()
}
