package mailbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"email-client/pkg/message"
)

// Entry is one saved message. ID is local and never reused; POP3Num is
// only the number the server gave it in the session that downloaded it.
type Entry struct {
	ID      int       `json:"id"`
	POP3Num int       `json:"pop3_num"`
	Hash    string    `json:"hash"`
	SavedAt time.Time `json:"saved_at"`
	message.Message
	Raw string `json:"raw"`
}

type file struct {
	NextID   int     `json:"next_id"`
	Messages []Entry `json:"messages"`
}

type Mailbox struct {
	path string
	data file
}

// Open loads the mailbox at path. A missing file gives an empty mailbox.
func Open(path string) (*Mailbox, error) {
	mb := &Mailbox{path: path, data: file{NextID: 1}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return mb, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	if err := json.Unmarshal(raw, &mb.data); err != nil {
		return nil, fmt.Errorf("mailbox: %s is corrupt: %w", path, err)
	}
	return mb, nil
}

// Add parses raw and stores it. If the same message is already saved,
// it returns the existing entry and added=false.
func (mb *Mailbox) Add(raw []byte, pop3Num int) (e Entry, added bool, err error) {
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	for _, old := range mb.data.Messages {
		if old.Hash == hash {
			return old, false, nil
		}
	}

	msg, err := message.Parse(raw)
	if err != nil {
		return Entry{}, false, err
	}
	e = Entry{
		ID:      mb.data.NextID,
		POP3Num: pop3Num,
		Hash:    hash,
		SavedAt: time.Now(),
		Message: *msg,
		Raw:     string(raw),
	}
	mb.data.NextID++
	mb.data.Messages = append(mb.data.Messages, e)
	return e, true, nil
}

// Save writes to a temp file and renames it, so a crash mid-write
// cannot leave a half-written mailbox.
func (mb *Mailbox) Save() error {
	if err := os.MkdirAll(filepath.Dir(mb.path), 0o755); err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	out, err := json.MarshalIndent(mb.data, "", "  ")
	if err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	tmp := mb.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	if err := os.Rename(tmp, mb.path); err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	return nil
}

func (mb *Mailbox) List() []Entry {
	return slices.Clone(mb.data.Messages)
}

func (mb *Mailbox) Get(id int) (Entry, bool) {
	for _, e := range mb.data.Messages {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
