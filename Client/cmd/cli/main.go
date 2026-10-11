package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"email-client/pkg/account"
	"email-client/pkg/config"
	"email-client/pkg/logger"
	"email-client/pkg/mailbox"
	"email-client/pkg/mailerr"
	"email-client/pkg/message"
)

const menu = `
1. Check Inbox
2. Read Message
3. Compose and Send Email
4. View Local Messages
5. Exit`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.json", "path to config file")
	debug := flag.Bool("debug", false, "write SMTP/POP3 protocol lines to the log")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.Password == "" {
		return fmt.Errorf("no password: set \"password\" in %s or the %s variable", *configPath, config.PasswordEnv)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log, closer, err := logger.Open(cfg.LogPath, level)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer closer.Close()

	acct, err := account.Open(cfg, log)
	if err != nil {
		return err
	}

	ui := &cli{in: bufio.NewScanner(os.Stdin), out: os.Stdout, acct: acct, log: log}
	fmt.Fprintf(ui.out, "Email client — %s via %s\n", cfg.Username, cfg.Server)
	ui.loop()
	return nil
}

type cli struct {
	in   *bufio.Scanner
	out  io.Writer
	acct *account.Account
	log  *slog.Logger
}

// prompt prints label and returns the trimmed input line. ok is false on end of input.
func (c *cli) prompt(label string) (line string, ok bool) {
	fmt.Fprint(c.out, label)
	if !c.in.Scan() {
		return "", false
	}
	return strings.TrimSpace(c.in.Text()), true
}

func (c *cli) loop() {
	for {
		fmt.Fprintln(c.out, menu)
		choice, ok := c.prompt("> ")
		if !ok {
			return
		}

		var err error
		switch choice {
		case "1":
			err = c.checkInbox()
		case "2":
			err = c.readMessage()
		case "3":
			err = c.compose()
		case "4":
			err = c.viewLocal()
		case "5":
			return
		default:
			err = mailerr.Input("invalid choice %q, enter 1-5", choice)
		}
		if err != nil {
			c.report(err)
		}
	}
}

// report prints a readable error and logs the full one with its kind.
func (c *cli) report(err error) {
	kind, headline, detail := mailerr.Explain(err)
	c.log.Error(headline, "kind", kind, "err", err)
	fmt.Fprintln(c.out, "Error:", headline)
	if detail != "" {
		fmt.Fprintln(c.out, "  detail:", detail)
	}
}

func (c *cli) checkInbox() error {
	items, added, err := c.acct.Refresh()
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "\n%d message(s) on server, %d new saved locally\n\n", len(items), added)

	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "No.\tFrom\tSubject\tDate")
	for _, it := range items {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", it.Num, cut(it.From, 30), cut(it.Subject, 40), date(it.Date))
	}
	return w.Flush()
}

func (c *cli) readMessage() error {
	s, ok := c.prompt("Message number (from Check Inbox): ")
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return mailerr.Input("%q is not a message number", s)
	}
	e, err := c.acct.Fetch(n)
	if err != nil {
		return err
	}
	c.show(e)
	return nil
}

func (c *cli) compose() error {
	toLine, ok := c.prompt("To (comma-separated): ")
	if !ok {
		return nil
	}
	to, err := message.ParseRecipients(toLine)
	if err != nil {
		return err
	}
	subject, ok := c.prompt("Subject: ")
	if !ok {
		return nil
	}

	fmt.Fprintln(c.out, "Body (end with a line containing only \".\"):")
	var body []string
	for c.in.Scan() {
		line := c.in.Text()
		if line == "." {
			break
		}
		body = append(body, line)
	}

	start := time.Now()
	if err := c.acct.Send(to, subject, strings.Join(body, "\n")); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Sent to %s in %d ms\n", strings.Join(to, ", "), time.Since(start).Milliseconds())
	return nil
}

func (c *cli) viewLocal() error {
	entries := c.acct.Local()
	if len(entries) == 0 {
		fmt.Fprintln(c.out, "\nNo saved messages. Use Check Inbox first.")
		return nil
	}

	fmt.Fprintln(c.out)
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tFrom\tSubject\tDate")
	for _, e := range entries {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", e.ID, cut(e.From, 30), cut(e.Subject, 40), date(e.Date))
	}
	w.Flush()

	s, ok := c.prompt("\nID to open (Enter to go back): ")
	if !ok || s == "" {
		return nil
	}
	id, err := strconv.Atoi(s)
	if err != nil {
		return mailerr.Input("%q is not an ID", s)
	}
	e, found := c.acct.LocalGet(id)
	if !found {
		return mailerr.Input("no saved message with ID %d", id)
	}
	c.show(e)
	return nil
}

func (c *cli) show(e mailbox.Entry) {
	fmt.Fprintf(c.out, "\nFrom:    %s\nTo:      %s\nSubject: %s\nDate:    %s\n\n%s\n",
		e.From, e.To, e.Subject, date(e.Date), e.Body)
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func date(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Local().Format("2006-01-02 15:04")
}
