package cli_test

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Builds the real CLI binary and drives it through stdin against GreenMail.

func build(t *testing.T) (bin, dir string) {
	t.Helper()
	for _, addr := range []string{"localhost:3025", "localhost:3110"} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Skipf("GreenMail not reachable at %s", addr)
		}
		conn.Close()
	}

	dir = t.TempDir()
	bin = filepath.Join(dir, "cli")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/cli")
	cmd.Dir = "../../Client"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cfg, err := os.ReadFile("../../Client/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func run(t *testing.T, bin, dir, input string) string {
	t.Helper()
	cmd := exec.Command(bin, "-debug")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli: %v\n%s", err, out)
	}
	return string(out)
}

func TestSendReceiveReadLocal(t *testing.T) {
	bin, dir := build(t)
	subject := fmt.Sprintf("cli-test-%d", time.Now().UnixNano())

	out := run(t, bin, dir, strings.Join([]string{
		"3", "alice@localhost", subject, "first line", ".dot line", ".",
		"1",
		"5",
	}, "\n")+"\n")
	if !strings.Contains(out, "Sent to alice@localhost") {
		t.Fatalf("send failed:\n%s", out)
	}
	if !strings.Contains(out, subject) {
		t.Fatalf("inbox does not list %s:\n%s", subject, out)
	}

	// Second process = restart. Open the newest local entry.
	again := run(t, bin, dir, "4\n\n5\n")
	if !strings.Contains(again, subject) {
		t.Fatalf("local view after restart misses %s:\n%s", subject, again)
	}

	log, err := os.ReadFile(filepath.Join(dir, "logs", "client.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "PASS alice") {
		t.Fatal("password written to log")
	}
	for _, want := range []string{"Connected to SMTP server", "Message sent successfully", "Retrieved message"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log missing %q", want)
		}
	}
}

func TestInvalidInputDoesNotCrash(t *testing.T) {
	bin, dir := build(t)
	out := run(t, bin, dir, "9\n2\nabc\n3\nnot-an-address\n4\n\n5\n")

	for _, want := range []string{
		`invalid choice "9"`,
		`"abc" is not a message number`,
		`invalid recipient "not-an-address"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// writeConfig overwrites the copied config.json with different server ports.
func writeConfig(t *testing.T, dir string, smtpPort, pop3Port int) {
	t.Helper()
	cfg := fmt.Sprintf(`{"server":"127.0.0.1","smtp_port":%d,"pop3_port":%d,
		"username":"alice@localhost","password":"alice","from":"alice@localhost","timeout_seconds":1}`, smtpPort, pop3Port)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestErrorCases(t *testing.T) {
	bin, dir := build(t)

	t.Run("wrong password", func(t *testing.T) {
		const bad = "not-the-password"
		cmd := exec.Command(bin, "-debug")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "EMAIL_PASSWORD="+bad)
		cmd.Stdin = strings.NewReader("1\n5\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "Login failed: wrong username or password.") {
			t.Errorf("output:\n%s", out)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "logs", "client.log"))
		if strings.Contains(string(log), bad) {
			t.Error("password written to log")
		}
	})

	t.Run("server unreachable", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		writeConfig(t, dir, port, port)

		out := run(t, bin, dir, "1\n3\nbob@localhost\nhi\nbody\n.\n5\n")
		if strings.Count(out, "Cannot reach the mail server") != 2 {
			t.Errorf("want 2 unreachable errors (inbox + send):\n%s", out)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				defer c.Close()
			}
		}()
		port := ln.Addr().(*net.TCPAddr).Port
		writeConfig(t, dir, port, port)

		start := time.Now()
		out := run(t, bin, dir, "1\n5\n")
		if !strings.Contains(out, "did not answer in time") {
			t.Errorf("output:\n%s", out)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("took %v with a 1s timeout", d)
		}
	})
}
