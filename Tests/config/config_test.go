package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"email-client/pkg/config"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeFile(t, `{"server":"localhost","smtp_port":3025,"pop3_port":3110,
		"username":"alice@localhost","password":"pw","from":"alice@localhost"}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPAddr() != "localhost:3025" || c.POP3Addr() != "localhost:3110" {
		t.Errorf("addrs = %s, %s", c.SMTPAddr(), c.POP3Addr())
	}
	if c.TimeoutSeconds != 10 {
		t.Errorf("default timeout = %d, want 10", c.TimeoutSeconds)
	}
}

func TestLoadPasswordFromEnv(t *testing.T) {
	path := writeFile(t, `{"server":"localhost","smtp_port":25,"pop3_port":110,
		"username":"u","password":"file-pw","from":"u@x"}`)
	t.Setenv(config.PasswordEnv, "env-pw")

	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != "env-pw" {
		t.Errorf("password = %q, want env-pw", c.Password)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := map[string]string{
		"missing server": `{"smtp_port":25,"pop3_port":110,"username":"u","from":"u@x"}`,
		"bad port":       `{"server":"h","smtp_port":70000,"pop3_port":110,"username":"u","from":"u@x"}`,
		"bad from":       `{"server":"h","smtp_port":25,"pop3_port":110,"username":"u","from":"nobody"}`,
		"unknown field":  `{"server":"h","smtp_port":25,"pop3_port":110,"username":"u","from":"u@x","smpt_port":1}`,
		"not json":       `server = h`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(writeFile(t, body))
			if err == nil || !strings.HasPrefix(err.Error(), "config:") {
				t.Errorf("err = %v, want config error", err)
			}
		})
	}
}
