package mailbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"email-client/pkg/mailbox"
)

const mailA = "From: bob@x\nSubject: one\n\nfirst\n"
const mailB = "From: carol@x\nSubject: two\n\nsecond\n"

func TestSaveAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "mailbox.json")

	mb, err := mailbox.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := mb.Add([]byte(mailA), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := mb.Add([]byte(mailB), 2)
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids = %d, %d", a.ID, b.ID)
	}
	if err := mb.Save(); err != nil {
		t.Fatal(err)
	}

	// Simulates a restart: a fresh Mailbox reading the same file.
	again, err := mailbox.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Get(2)
	if !ok || got.Subject != "two" || got.From != "carol@x" || got.Body != "second\n" || got.Raw != mailB {
		t.Fatalf("reloaded entry = %+v", got)
	}
	if len(again.List()) != 2 {
		t.Errorf("List len = %d", len(again.List()))
	}

	// POP3 numbers restart every session; the local id must keep counting.
	c, added, _ := again.Add([]byte("From: dan@x\nSubject: three\n\nthird\n"), 1)
	if !added || c.ID != 3 {
		t.Errorf("new entry id = %d added=%v, want 3 true", c.ID, added)
	}
}

func TestDuplicateNotAdded(t *testing.T) {
	mb, _ := mailbox.Open(filepath.Join(t.TempDir(), "mb.json"))
	first, _, _ := mb.Add([]byte(mailA), 1)
	dup, added, err := mb.Add([]byte(mailA), 5)
	if err != nil || added || dup.ID != first.ID {
		t.Fatalf("dup = %+v added=%v err=%v", dup, added, err)
	}
	if len(mb.List()) != 1 {
		t.Errorf("List len = %d, want 1", len(mb.List()))
	}
}

func TestCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mb.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := mailbox.Open(path); err == nil {
		t.Fatal("expected error for corrupt file")
	}
}
