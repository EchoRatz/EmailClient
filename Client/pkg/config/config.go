package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// PasswordEnv overrides the password in the file, so it can stay out of config.json.
const PasswordEnv = "EMAIL_PASSWORD"

type Config struct {
	Server         string `json:"server"`
	SMTPPort       int    `json:"smtp_port"`
	POP3Port       int    `json:"pop3_port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	From           string `json:"from"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MailboxPath    string `json:"mailbox_path"`
	LogPath        string `json:"log_path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if pw, ok := os.LookupEnv(PasswordEnv); ok {
		c.Password = pw
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 10
	}
	if c.MailboxPath == "" {
		c.MailboxPath = "data/mailbox.json"
	}
	if c.LogPath == "" {
		c.LogPath = "logs/client.log"
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Server) == "" {
		errs = append(errs, errors.New("server is required"))
	}
	if c.SMTPPort < 1 || c.SMTPPort > 65535 {
		errs = append(errs, fmt.Errorf("smtp_port %d out of range 1-65535", c.SMTPPort))
	}
	if c.POP3Port < 1 || c.POP3Port > 65535 {
		errs = append(errs, fmt.Errorf("pop3_port %d out of range 1-65535", c.POP3Port))
	}
	if strings.TrimSpace(c.Username) == "" {
		errs = append(errs, errors.New("username is required"))
	}
	if !strings.Contains(c.From, "@") {
		errs = append(errs, fmt.Errorf("from %q is not an email address", c.From))
	}
	if c.TimeoutSeconds < 0 {
		errs = append(errs, errors.New("timeout_seconds must be positive"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return nil
}

func (c *Config) SMTPAddr() string {
	return net.JoinHostPort(c.Server, strconv.Itoa(c.SMTPPort))
}

func (c *Config) POP3Addr() string {
	return net.JoinHostPort(c.Server, strconv.Itoa(c.POP3Port))
}

func (c *Config) Timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}
