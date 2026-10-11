package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"email-client/pkg/account"
	"email-client/pkg/config"
	"email-client/pkg/mailbox"
	"email-client/pkg/mailerr"
	"email-client/pkg/message"
	"email-client/pkg/pop3"
	"email-client/pkg/smtp"
)

// Server is the HTTP API. cfg supplies the mail server address, ports,
// timeout and data folder; the account comes from whoever signs in.
type Server struct {
	cfg *config.Config
	log *slog.Logger
	mux *http.ServeMux

	// Verify checks a mail login. It defaults to a real POP3 USER/PASS;
	// tests replace it to sign in without a mail server.
	Verify func(email, password string) error

	mu       sync.Mutex
	sessions map[string]session
	accounts map[string]*account.Account
}

// New builds the API. If webDir is not empty, non-API paths serve the
// built web UI from that folder.
func New(cfg *config.Config, log *slog.Logger, webDir string) *Server {
	s := &Server{
		cfg:      cfg,
		log:      log,
		mux:      http.NewServeMux(),
		sessions: map[string]session{},
		accounts: map[string]*account.Account{},
	}
	s.Verify = s.pop3Login

	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("POST /api/logout", s.logout)
	s.mux.HandleFunc("GET /api/me", s.authed(s.me))
	s.mux.HandleFunc("GET /api/inbox", s.authed(s.inbox))
	s.mux.HandleFunc("POST /api/refresh", s.authed(s.refresh))
	s.mux.HandleFunc("GET /api/messages/{num}", s.authed(s.message))
	s.mux.HandleFunc("POST /api/send", s.authed(s.send))
	s.mux.HandleFunc("GET /api/local", s.authed(s.local))
	s.mux.HandleFunc("GET /api/avatar", s.authed(s.avatar))
	s.mux.HandleFunc("PUT /api/avatar", s.authed(s.setAvatar))
	if webDir != "" {
		s.mux.Handle("/", http.FileServer(http.Dir(webDir)))
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

type summary struct {
	Num     int       `json:"num,omitempty"`
	ID      int       `json:"id"`
	From    string    `json:"from"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
}

type full struct {
	ID      int       `json:"id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Body    string    `json:"body"`
}

func toFull(e mailbox.Entry) full {
	return full{ID: e.ID, From: e.From, To: e.To, Subject: e.Subject, Date: e.Date, Body: e.Body}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

var statusFor = map[mailerr.Kind]int{
	mailerr.InvalidInput: http.StatusBadRequest,
	mailerr.Timeout:      http.StatusGatewayTimeout,
	mailerr.Unknown:      http.StatusInternalServerError,
}

// fail sends {"error","kind","detail"}. Mail-server problems are 502 (bad
// gateway): this API is fine, the server behind it is not.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	kind, headline, detail := mailerr.Explain(err)
	status, ok := statusFor[kind]
	if !ok {
		status = http.StatusBadGateway
	}
	s.log.Error(headline, "kind", kind, "path", r.URL.Path, "err", err)
	body := map[string]string{"error": headline, "kind": string(kind)}
	if detail != "" {
		body["detail"] = detail
	}
	writeJSON(w, status, body)
}

func status(addr string, err error) map[string]string {
	if err != nil {
		kind, headline, _ := mailerr.Explain(err)
		return map[string]string{"addr": addr, "status": "down", "kind": string(kind), "error": headline}
	}
	return map[string]string{"addr": addr, "status": "up"}
}

// health checks that each server sends its greeting. It does not log in,
// so it works before anyone has signed in.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	smtpErr := func() error {
		c, err := smtp.Dial(s.cfg.SMTPAddr(), timeout, s.log)
		if err != nil {
			return err
		}
		return c.Quit()
	}()
	pop3Err := func() error {
		c, err := pop3.Dial(s.cfg.POP3Addr(), timeout, s.log)
		if err != nil {
			return err
		}
		return c.Quit()
	}()
	writeJSON(w, http.StatusOK, map[string]any{
		"server": s.cfg.Server,
		"smtp":   status(s.cfg.SMTPAddr(), smtpErr),
		"pop3":   status(s.cfg.POP3Addr(), pop3Err),
	})
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request, u user) {
	items, _, err := u.acct.Refresh()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]summary, 0, len(items))
	for _, it := range items {
		out = append(out, summary{Num: it.Num, ID: it.ID, From: it.From, Subject: it.Subject, Date: it.Date})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request, u user) {
	start := time.Now()
	items, added, err := u.acct.Refresh()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"on_server": len(items),
		"added":     added,
		"ms":        int(time.Since(start).Milliseconds()),
	})
}

func (s *Server) message(w http.ResponseWriter, r *http.Request, u user) {
	num, err := strconv.Atoi(r.PathValue("num"))
	if err != nil || num < 1 {
		s.fail(w, r, mailerr.Input("message number must be a positive integer"))
		return
	}
	e, err := u.acct.Fetch(num)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFull(e))
}

type sendRequest struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

func (s *Server) send(w http.ResponseWriter, r *http.Request, u user) {
	var req sendRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.fail(w, r, mailerr.Input("request body must be JSON {to:[...], subject, body}: %v", err))
		return
	}
	to, err := message.ParseRecipients(strings.Join(req.To, ","))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	start := time.Now()
	if err := u.acct.Send(to, req.Subject, req.Body); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": to, "ms": time.Since(start).Milliseconds()})
}

func (s *Server) local(w http.ResponseWriter, r *http.Request, u user) {
	entries := u.acct.Local()
	out := make([]full, 0, len(entries))
	for _, e := range entries {
		out = append(out, toFull(e))
	}
	writeJSON(w, http.StatusOK, out)
}
