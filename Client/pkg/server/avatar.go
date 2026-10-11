package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"email-client/pkg/mailerr"
)

const maxAvatar = 2 << 20

// The type is read from the file's first bytes, not from the request header,
// which the browser (or anyone with curl) can set to anything.
var avatarTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func (s *Server) avatarPath(key string) string {
	return filepath.Join(s.userDir(key), "avatar")
}

func (s *Server) avatar(w http.ResponseWriter, r *http.Request, u user) {
	data, err := os.ReadFile(s.avatarPath(u.key))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "No profile picture.", "kind": "not_found"})
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *Server) setAvatar(w http.ResponseWriter, r *http.Request, u user) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAvatar))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		s.fail(w, r, mailerr.Input("Pictures must be 2 MB or smaller."))
		return
	case err != nil:
		s.fail(w, r, err)
		return
	case !avatarTypes[http.DetectContentType(data)]:
		s.fail(w, r, mailerr.Input("Choose a PNG, JPEG, GIF or WebP image."))
		return
	}

	path := s.avatarPath(u.key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("Profile picture changed", "user", u.email, "bytes", len(data))
	s.me(w, r, u)
}
