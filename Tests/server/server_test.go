package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"email-client/pkg/config"
	"email-client/pkg/server"
)

// api is one browser: a test server plus a client with its own cookie jar.
type api struct {
	t      *testing.T
	url    string
	client *http.Client
}

// newAPI starts the server. fakeLogin accepts any password, for tests whose
// mail server is fake or down and so cannot check one.
func newAPI(t *testing.T, cfg *config.Config, log *slog.Logger, fakeLogin bool) *api {
	t.Helper()
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	cfg.MailboxPath = filepath.Join(t.TempDir(), "mailbox.json")
	s := server.New(cfg, log, "")
	if fakeLogin {
		s.Verify = func(string, string) error { return nil }
	}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return (&api{t: t, url: ts.URL}).browser()
}

// browser returns a second client on the same server with no cookies.
func (a *api) browser() *api {
	jar, _ := cookiejar.New(nil)
	return &api{t: a.t, url: a.url, client: &http.Client{Jar: jar}}
}

// call sends a request and decodes the JSON reply into out (if not nil).
func (a *api) call(method, path, body string, out any) int {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, strings.NewReader(body))
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			a.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (a *api) login(email, password string) int {
	a.t.Helper()
	return a.call("POST", "/api/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), nil)
}

func (a *api) mustLogin(email, password string) *api {
	a.t.Helper()
	if code := a.login(email, password); code != 200 {
		a.t.Fatalf("login %s: status %d", email, code)
	}
	return a
}

func greenmail(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../Client/config.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{cfg.SMTPAddr(), cfg.POP3Addr()} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Skipf("GreenMail not reachable at %s", addr)
		}
		conn.Close()
	}
	return cfg
}

func fakeConfig(port int) *config.Config {
	return &config.Config{Server: "127.0.0.1", SMTPPort: port, POP3Port: port, Username: "u", From: "u@x", TimeoutSeconds: 1}
}

type health struct {
	SMTP map[string]string `json:"smtp"`
	POP3 map[string]string `json:"pop3"`
}

type apiError struct {
	Error  string `json:"error"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type me struct {
	Email     string `json:"email"`
	HasAvatar bool   `json:"has_avatar"`
}

func TestRoundTrip(t *testing.T) {
	a := newAPI(t, greenmail(t), nil, false).mustLogin("alice@localhost", "alice")
	subject := fmt.Sprintf("api-test-%d", time.Now().UnixNano())

	var h health
	a.call("GET", "/api/health", "", &h)
	if h.SMTP["status"] != "up" || h.POP3["status"] != "up" {
		t.Fatalf("health = %+v", h)
	}

	body := fmt.Sprintf(`{"to":["alice@localhost"],"subject":%q,"body":"via API\n.dot"}`, subject)
	if code := a.call("POST", "/api/send", body, nil); code != 200 {
		t.Fatalf("send status %d", code)
	}

	var inbox []struct {
		Num     int    `json:"num"`
		From    string `json:"from"`
		Subject string `json:"subject"`
	}
	a.call("GET", "/api/inbox", "", &inbox)
	num := 0
	for _, m := range inbox {
		if m.Subject == subject {
			num = m.Num
			if m.From != "alice@localhost" {
				t.Errorf("From = %q, want the signed-in address", m.From)
			}
		}
	}
	if num == 0 {
		t.Fatalf("sent message not in inbox of %d", len(inbox))
	}

	var msg struct{ Subject, Body string }
	if code := a.call("GET", "/api/messages/"+strconv.Itoa(num), "", &msg); code != 200 {
		t.Fatalf("message status %d", code)
	}
	if msg.Subject != subject || msg.Body != "via API\n.dot\n" {
		t.Errorf("message = %+v", msg)
	}

	var refresh map[string]int
	a.call("POST", "/api/refresh", "", &refresh)
	if refresh["added"] != 0 || refresh["on_server"] != len(inbox) {
		t.Errorf("second refresh = %v, want 0 added", refresh)
	}

	var local []struct{ Subject string }
	a.call("GET", "/api/local", "", &local)
	if len(local) != len(inbox) {
		t.Errorf("local has %d, inbox %d", len(local), len(inbox))
	}
}

func TestLoginFlow(t *testing.T) {
	a := newAPI(t, greenmail(t), nil, false)

	var e apiError
	if code := a.call("GET", "/api/inbox", "", &e); code != 401 || e.Kind != "unauthenticated" {
		t.Errorf("inbox before login: %d %+v", code, e)
	}
	if code := a.call("GET", "/api/health", "", nil); code != 200 {
		t.Errorf("health must work signed out, got %d", code)
	}
	if code := a.call("POST", "/api/login", `{"email":"alice@localhost","password":"wrong"}`, &e); code != 401 || e.Kind != "auth_failed" {
		t.Errorf("wrong password: %d %+v", code, e)
	}
	for _, body := range []string{`{"email":"alice","password":"x"}`, `{"email":"alice@localhost","password":""}`, `nope`} {
		if code := a.call("POST", "/api/login", body, &e); code != 400 || e.Kind != "invalid_input" {
			t.Errorf("login %s: %d %+v", body, code, e)
		}
	}

	var m me
	if code := a.call("POST", "/api/login", `{"email":"alice@localhost","password":"alice"}`, &m); code != 200 || m.Email != "alice@localhost" {
		t.Fatalf("login: %d %+v", code, m)
	}
	a.call("GET", "/api/me", "", &m)
	if m.Email != "alice@localhost" {
		t.Errorf("me = %+v", m)
	}

	// A second browser signed in as bob sees bob, while alice stays alice.
	b := a.browser().mustLogin("bob@localhost", "bob")
	b.call("GET", "/api/me", "", &m)
	if m.Email != "bob@localhost" {
		t.Errorf("bob's me = %+v", m)
	}
	if code := b.call("GET", "/api/inbox", "", nil); code != 200 {
		t.Errorf("bob's inbox: %d", code)
	}
	a.call("GET", "/api/me", "", &m)
	if m.Email != "alice@localhost" {
		t.Errorf("alice's me after bob signed in = %+v", m)
	}

	if code := a.call("POST", "/api/logout", "", nil); code != 204 {
		t.Errorf("logout: %d", code)
	}
	if code := a.call("GET", "/api/me", "", nil); code != 401 {
		t.Errorf("me after logout: %d", code)
	}
	if code := b.call("GET", "/api/me", "", nil); code != 200 {
		t.Errorf("bob was signed out by alice's logout: %d", code)
	}
}

// Parallel requests must not race on the mailbox or open two POP3 sessions.
// Run with -race to check.
func TestConcurrentRequests(t *testing.T) {
	a := newAPI(t, greenmail(t), nil, false).mustLogin("alice@localhost", "alice")
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := range 12 {
		wg.Go(func() {
			switch i % 3 {
			case 0:
				codes <- a.call("POST", "/api/refresh", "", nil)
			case 1:
				codes <- a.call("GET", "/api/local", "", nil)
			case 2:
				codes <- a.call("POST", "/api/send",
					fmt.Sprintf(`{"to":["bob@localhost"],"subject":"concurrent %d","body":"x"}`, i), nil)
			}
		})
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Errorf("status %d", c)
		}
	}
}

func TestBadInput(t *testing.T) {
	a := newAPI(t, fakeConfig(1), nil, true).mustLogin("u@x", "p")

	cases := []struct{ method, path, body string }{
		{"POST", "/api/send", `{"to":["nope"],"subject":"s","body":"b"}`},
		{"POST", "/api/send", `{"to":[],"subject":"s","body":"b"}`},
		{"POST", "/api/send", `{"too":["a@x"]}`},
		{"POST", "/api/send", `not json`},
		{"GET", "/api/messages/abc", ""},
		{"GET", "/api/messages/0", ""},
	}
	for _, c := range cases {
		var e apiError
		code := a.call(c.method, c.path, c.body, &e)
		if code != 400 || e.Kind != "invalid_input" || e.Error == "" {
			t.Errorf("%s %s %s: %d %+v", c.method, c.path, c.body, code, e)
		}
	}

	if code := a.call("GET", "/api/send", "", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/send = %d, want 405", code)
	}
}

func closedPort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestServerDown(t *testing.T) {
	port := closedPort(t)
	a := newAPI(t, fakeConfig(port), nil, true).mustLogin("u@x", "p")

	var e apiError
	code := a.call("POST", "/api/refresh", "", &e)
	if code != 502 || e.Kind != "unreachable" || !strings.Contains(e.Detail, "connection refused") {
		t.Errorf("refresh: %d %+v", code, e)
	}
	code = a.call("POST", "/api/send", `{"to":["a@x"],"subject":"s","body":"b"}`, &e)
	if code != 502 || e.Kind != "unreachable" {
		t.Errorf("send: %d %+v", code, e)
	}

	var h health
	a.call("GET", "/api/health", "", &h)
	if h.SMTP["status"] != "down" || h.POP3["kind"] != "unreachable" {
		t.Errorf("health = %+v", h)
	}

	// With the real POP3 check, signing in fails as unreachable, not as a bad password.
	strict := newAPI(t, fakeConfig(port), nil, false)
	if code := strict.call("POST", "/api/login", `{"email":"u@x","password":"p"}`, &e); code != 502 || e.Kind != "unreachable" {
		t.Errorf("login with server down: %d %+v", code, e)
	}
}

func TestTimeout(t *testing.T) {
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
	a := newAPI(t, fakeConfig(ln.Addr().(*net.TCPAddr).Port), nil, true).mustLogin("u@x", "p")

	var e apiError
	start := time.Now()
	code := a.call("POST", "/api/refresh", "", &e)
	if code != 504 || e.Kind != "timeout" {
		t.Errorf("refresh: %d %+v", code, e)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v, timeout is 1s", d)
	}
}

func TestWrongPasswordNotLogged(t *testing.T) {
	const bad = "wrong-pw-should-not-appear"
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := newAPI(t, greenmail(t), log, false)

	if code := a.login("alice@localhost", bad); code != 401 {
		t.Errorf("login: %d", code)
	}
	a.mustLogin("alice@localhost", "alice")
	if strings.Contains(buf.String(), bad) || strings.Contains(buf.String(), "PASS alice") {
		t.Fatalf("password leaked into log:\n%s", buf.String())
	}
}

func pngBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAvatar(t *testing.T) {
	a := newAPI(t, fakeConfig(1), nil, true).mustLogin("a@x", "p")
	b := a.browser().mustLogin("b@x", "p")
	pic := pngBytes(t)

	var e apiError
	if code := a.call("GET", "/api/avatar", "", &e); code != 404 {
		t.Errorf("avatar before upload: %d", code)
	}

	var m me
	if code := a.call("PUT", "/api/avatar", string(pic), &m); code != 200 || !m.HasAvatar {
		t.Fatalf("upload: %d %+v", code, m)
	}
	resp, err := a.client.Get(a.url + "/api/avatar")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, pic) || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("avatar: %d bytes, type %q", len(got), resp.Header.Get("Content-Type"))
	}

	if code := b.call("GET", "/api/avatar", "", &e); code != 404 {
		t.Errorf("b sees a's picture: %d", code)
	}
	if code := a.call("PUT", "/api/avatar", "<svg onload=alert(1)>", &e); code != 400 || e.Kind != "invalid_input" {
		t.Errorf("non-image upload: %d %+v", code, e)
	}
	big := append(pngBytes(t), make([]byte, 3<<20)...)
	if code := a.call("PUT", "/api/avatar", string(big), &e); code != 400 || !strings.Contains(e.Error, "2 MB") {
		t.Errorf("3 MB upload: %d %+v", code, e)
	}
	if code := newAPI(t, fakeConfig(1), nil, true).call("PUT", "/api/avatar", string(pic), nil); code != 401 {
		t.Errorf("upload signed out: %d", code)
	}
}
