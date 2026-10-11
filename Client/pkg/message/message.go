package message

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	"email-client/pkg/mailerr"
)

type Message struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Body    string    `json:"body"`
}

// Parse reads the headers and plain-text body of a raw RFC 5322 message.
func Parse(raw []byte) (*Message, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("message: %w", err)
	}
	body, err := io.ReadAll(m.Body)
	if err != nil {
		return nil, fmt.Errorf("message: body: %w", err)
	}

	dec := new(mime.WordDecoder)
	header := func(key string) string {
		v := m.Header.Get(key)
		if d, err := dec.DecodeHeader(v); err == nil {
			return d
		}
		return v
	}
	date, _ := m.Header.Date()

	return &Message{
		From:    header("From"),
		To:      header("To"),
		Subject: header("Subject"),
		Date:    date,
		Body:    string(body),
	}, nil
}

// Build produces the text sent after SMTP DATA. Line endings are left as \n;
// the SMTP DotWriter turns them into \r\n.
func Build(from string, to []string, subject, body string, date time.Time) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\n", from)
	fmt.Fprintf(&b, "To: %s\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\n", date.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\n")
	b.WriteString("\n")

	body = strings.ReplaceAll(body, "\r\n", "\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.Bytes()
}

// ParseRecipients splits a comma-separated "To" field into bare addresses,
// rejecting anything net/mail cannot parse.
func ParseRecipients(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := mail.ParseAddress(part)
		if err != nil {
			return nil, mailerr.Input("invalid recipient %q", part)
		}
		out = append(out, addr.Address)
	}
	if len(out) == 0 {
		return nil, mailerr.Input("at least one recipient is required")
	}
	return out, nil
}
