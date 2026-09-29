package cache

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-proton-api"
)

func TestCache(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	m1 := proton.MessageMetadata{ID: "1", ConversationID: "A", Time: 100, Subject: "Tajný předmět", LabelIDs: []string{"0", "5"}}
	m2 := proton.MessageMetadata{ID: "2", ConversationID: "A", Time: 200, Subject: "Druhá", LabelIDs: []string{"7", "5"}}
	if err := c.PutMetadata(m1, m2); err != nil {
		t.Fatal(err)
	}
	if err := c.PutMessage(proton.Message{MessageMetadata: m1, Body: "-----BEGIN PGP MESSAGE-----x"}); err != nil {
		t.Fatal(err)
	}
	inbox, _ := c.List("0", 0, 50)
	if len(inbox) != 1 || inbox[0].Subject != "Tajný předmět" {
		t.Errorf("inbox = %+v", inbox)
	}
	all, _ := c.List("5", 0, 50)
	if len(all) != 2 || all[0].ID != "2" {
		t.Errorf("all mail order wrong: %+v", all)
	}
	conv, _ := c.Conversation("A")
	if len(conv) != 2 || conv[0].ID != "1" {
		t.Errorf("conversation = %+v", conv)
	}
	// Label update keeps the cached body.
	m1.LabelIDs = []string{"6", "5"}
	c.PutMetadata(m1)
	full, ok := c.Message("1")
	if !ok || full.Body != "-----BEGIN PGP MESSAGE-----x" || full.LabelIDs[0] != "6" {
		t.Errorf("full message = %+v %v", full, ok)
	}
	var v []string
	c.Set("labels", []string{"x"})
	if !c.Get("labels", &v) || v[0] != "x" {
		t.Error("kv roundtrip failed")
	}
	c.Close()

	// Nothing sensitive is readable on disk.
	raw, _ := os.ReadFile(path)
	wal, _ := os.ReadFile(path + "-wal")
	for _, secret := range []string{"Tajný předmět", "BEGIN PGP"} {
		if bytes.Contains(raw, []byte(secret)) || bytes.Contains(wal, []byte(secret)) {
			t.Errorf("%q found in plaintext on disk", secret)
		}
	}
	// Wrong key cannot read it.
	bad := make([]byte, 32)
	c2, _ := Open(path, bad)
	if l, _ := c2.List("0", 0, 50); len(l) != 0 {
		t.Error("records readable with a wrong key")
	}
	c2.Close()
}
