package account

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"email-client/pkg/config"
	"email-client/pkg/mailbox"
	"email-client/pkg/message"
	"email-client/pkg/pop3"
	"email-client/pkg/smtp"
)

// Account ties the one configured mail account to its local mailbox.
// The CLI and the HTTP server both work through it.
type Account struct {
	cfg *config.Config
	log *slog.Logger
	mb  *mailbox.Mailbox

	// popMu allows one POP3 session at a time. Servers lock the maildrop
	// during a session, so a second parallel login would be refused.
	popMu sync.Mutex
}

// InboxItem is a server message: its POP3 number for this session plus the saved copy.
type InboxItem struct {
	Num int
	mailbox.Entry
}

func Open(cfg *config.Config, log *slog.Logger) (*Account, error) {
	mb, err := mailbox.Open(cfg.MailboxPath)
	if err != nil {
		return nil, err
	}
	return &Account{cfg: cfg, log: log, mb: mb}, nil
}

// SetPassword replaces the password after a successful re-login. Only POP3
// sessions read it, and they hold popMu, so taking popMu here is enough.
func (a *Account) SetPassword(pw string) {
	a.popMu.Lock()
	a.cfg.Password = pw
	a.popMu.Unlock()
}

func (a *Account) Send(to []string, subject, body string) error {
	raw := message.Build(a.cfg.From, to, subject, body, time.Now())
	return smtp.Send(a.cfg.SMTPAddr(), a.cfg.Timeout(), a.log, a.cfg.From, to, raw)
}

// session opens an authenticated POP3 connection, runs fn, then sends QUIT.
func (a *Account) session(fn func(*pop3.Client) error) error {
	a.popMu.Lock()
	defer a.popMu.Unlock()

	c, err := pop3.Dial(a.cfg.POP3Addr(), a.cfg.Timeout(), a.log)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(a.cfg.Username, a.cfg.Password); err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	return c.Quit()
}

// Refresh downloads every message on the server, saves new ones locally,
// and returns the inbox plus how many messages were new.
func (a *Account) Refresh() (items []InboxItem, added int, err error) {
	err = a.session(func(c *pop3.Client) error {
		list, err := c.List()
		if err != nil {
			return err
		}
		for _, m := range list {
			raw, err := c.Retr(m.Num)
			if err != nil {
				return err
			}
			e, isNew, err := a.mb.Add(raw, m.Num)
			if err != nil {
				return fmt.Errorf("message %d: %w", m.Num, err)
			}
			if isNew {
				added++
			}
			items = append(items, InboxItem{Num: m.Num, Entry: e})
		}
		return nil
	})
	if added > 0 {
		if serr := a.mb.Save(); serr != nil && err == nil {
			err = serr
		}
	}
	return items, added, err
}

// Fetch retrieves one message by its POP3 number and saves it locally.
func (a *Account) Fetch(num int) (mailbox.Entry, error) {
	var e mailbox.Entry
	err := a.session(func(c *pop3.Client) error {
		raw, err := c.Retr(num)
		if err != nil {
			return err
		}
		var isNew bool
		e, isNew, err = a.mb.Add(raw, num)
		if err != nil {
			return err
		}
		if isNew {
			return a.mb.Save()
		}
		return nil
	})
	return e, err
}

func (a *Account) Local() []mailbox.Entry {
	return a.mb.List()
}

func (a *Account) LocalGet(id int) (mailbox.Entry, bool) {
	return a.mb.Get(id)
}

func (a *Account) Email() string {
	return a.cfg.From
}
