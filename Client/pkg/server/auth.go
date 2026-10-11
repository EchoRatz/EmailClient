package server

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"email-client/pkg/account"
	"email-client/pkg/mailerr"
	"email-client/pkg/pop3"
)

const (
	cookieName = "mail_session"
	sessionTTL = 12 * time.Hour
)

type session struct {
	key     string
	expires time.Time
}

// user is the signed-in account a handler works on.
type user struct {
	key   string // lowercased email, used for map keys and folder names
	email string
	acct  *account.Account
}

// pop3Login proves the password by logging in for real, then QUITs.
func (s *Server) pop3Login(email, password string) error {
	c, err := pop3.Dial(s.cfg.POP3Addr(), s.cfg.Timeout(), s.log)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(email, password); err != nil {
		return err
	}
	return c.Quit()
}

// safeName turns an email into a folder name: lowercase, and anything other
// than letters, digits and . _ @ - becomes "_", so it can never contain "/".
func safeName(email string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', strings.ContainsRune("._@-", r):
			return r
		}
		return '_'
	}, strings.ToLower(email))
}

// userDir is data/accounts/<email>/, next to the CLI's mailbox file.
func (s *Server) userDir(key string) string {
	return filepath.Join(filepath.Dir(s.cfg.MailboxPath), "accounts", safeName(key))
}

// accountFor returns the one Account for this email, so two sessions of the
// same user share a mailbox and the POP3 lock. Called with s.mu held.
func (s *Server) accountFor(key, email, password string) (*account.Account, error) {
	if a, ok := s.accounts[key]; ok {
		a.SetPassword(password)
		return a, nil
	}
	c := *s.cfg
	c.Username, c.Password, c.From = email, password, email
	c.MailboxPath = filepath.Join(s.userDir(key), "mailbox.json")
	a, err := account.Open(&c, s.log)
	if err != nil {
		return nil, err
	}
	s.accounts[key] = a
	return a, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.fail(w, r, mailerr.Input("request body must be JSON {email, password}"))
		return
	}
	email := strings.TrimSpace(req.Email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		s.fail(w, r, mailerr.Input("Enter an email address like alice@localhost."))
		return
	}
	if req.Password == "" {
		s.fail(w, r, mailerr.Input("Enter your password."))
		return
	}

	if err := s.Verify(email, req.Password); err != nil {
		if mailerr.Classify(err) == mailerr.AuthFailed {
			_, headline, _ := mailerr.Explain(err)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": headline, "kind": string(mailerr.AuthFailed)})
			return
		}
		s.fail(w, r, err)
		return
	}

	token := rand.Text()
	key := strings.ToLower(email)

	s.mu.Lock()
	_, err := s.accountFor(key, email, req.Password)
	if err == nil {
		s.sessions[token] = session{key: key, expires: time.Now().Add(sessionTTL)}
	}
	s.mu.Unlock()
	if err != nil {
		s.fail(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	s.log.Info("Signed in", "user", email)
	s.me(w, r, user{key: key, email: email})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		if sess, ok := s.sessions[c.Value]; ok {
			s.log.Info("Signed out", "user", sess.key)
			delete(s.sessions, c.Value)
		}
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

// current finds the session cookie's account. Expired sessions are removed here.
func (s *Server) current(r *http.Request) (user, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return user{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[c.Value]
	if !ok {
		return user{}, false
	}
	if time.Now().After(sess.expires) {
		delete(s.sessions, c.Value)
		return user{}, false
	}
	a := s.accounts[sess.key]
	return user{key: sess.key, email: a.Email(), acct: a}, true
}

// authed lets a handler run only for a signed-in user.
func (s *Server) authed(h func(http.ResponseWriter, *http.Request, user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.current(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Sign in first.", "kind": "unauthenticated"})
			return
		}
		h(w, r, u)
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u user) {
	_, err := os.Stat(s.avatarPath(u.key))
	writeJSON(w, http.StatusOK, map[string]any{"email": u.email, "has_avatar": err == nil})
}
