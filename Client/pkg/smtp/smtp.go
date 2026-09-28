package smtp

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/textproto"
	"time"
)

type Client struct {
	conn    net.Conn
	tp      *textproto.Conn
	log     *slog.Logger
	timeout time.Duration
}

func Dial(addr string, timeout time.Duration, log *slog.Logger) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("smtp: connect %s: %w", addr, err)
	}
	c := &Client{conn: conn, tp: textproto.NewConn(conn), log: log, timeout: timeout}

	c.extend()
	code, msg, err := c.tp.ReadResponse(220)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp: greeting: %w", err)
	}
	c.log.Debug("S: "+msg, "code", code)
	c.log.Info("Connected to SMTP server", "addr", addr)
	return c, nil
}

// extend pushes the socket deadline forward so each step gets a full timeout.
func (c *Client) extend() {
	c.conn.SetDeadline(time.Now().Add(c.timeout))
}

func (c *Client) cmd(expect int, format string, args ...any) error {
	line := fmt.Sprintf(format, args...)
	c.extend()
	c.log.Debug("C: " + line)
	if err := c.tp.PrintfLine("%s", line); err != nil {
		return err
	}
	code, msg, err := c.tp.ReadResponse(expect)
	c.log.Debug("S: "+msg, "code", code)
	return err
}

func (c *Client) Hello(name string) error {
	err := c.cmd(250, "EHLO %s", name)
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 500 {
		err = c.cmd(250, "HELO %s", name)
	}
	if err != nil {
		return fmt.Errorf("smtp: HELO: %w", err)
	}
	return nil
}

func (c *Client) Mail(from string) error {
	if err := c.cmd(250, "MAIL FROM:<%s>", from); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	return nil
}

func (c *Client) Rcpt(to string) error {
	if err := c.cmd(250, "RCPT TO:<%s>", to); err != nil {
		return fmt.Errorf("smtp: RCPT TO %s: %w", to, err)
	}
	return nil
}

// Data sends DATA and then msg. The DotWriter converts \n to \r\n,
// dot-stuffs lines that start with ".", and writes the final ".\r\n".
func (c *Client) Data(msg []byte) error {
	if err := c.cmd(354, "DATA"); err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}

	c.extend()
	w := c.tp.DotWriter()
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: end body: %w", err)
	}

	code, reply, err := c.tp.ReadResponse(250)
	c.log.Debug("S: "+reply, "code", code)
	if err != nil {
		return fmt.Errorf("smtp: message rejected: %w", err)
	}
	return nil
}

func (c *Client) Quit() error {
	err := c.cmd(221, "QUIT")
	c.conn.Close()
	if err != nil {
		return fmt.Errorf("smtp: QUIT: %w", err)
	}
	return nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// Send runs one full session: connect, EHLO, MAIL FROM, RCPT TO per
// recipient, DATA, QUIT.
func Send(addr string, timeout time.Duration, log *slog.Logger, from string, to []string, msg []byte) error {
	if len(to) == 0 {
		return errors.New("smtp: no recipients")
	}

	c, err := Dial(addr, timeout, log)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Hello("email-client"); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	if err := c.Data(msg); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return err
	}

	log.Info("Message sent successfully", "to", to)
	return nil
}
