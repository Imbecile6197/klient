package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"path/filepath"
	"testing"

	"github.com/Imbecile6197/klient/internal/cache"
	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

type cacheAccount struct {
	mailbox.Account
	c *cache.Cache
}

func (f *cacheAccount) Cache() *cache.Cache { return f.c }

// Waiting messages survive a restart, attachments included, and stay with
// their own account.
func TestOutboxPersists(t *testing.T) {
	open := func(name string) *cache.Cache {
		c, err := cache.Open(filepath.Join(t.TempDir(), name), make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	acc1 := &cacheAccount{c: open("1.db")}
	acc2 := &cacheAccount{c: open("2.db")}
	a := &App{}
	d := &protonmail.Draft{
		Subject: "Faktura", Body: "Dobrý den",
		To:          []*mail.Address{{Name: "Jana", Address: "jana@example.org"}},
		Attachments: []*protonmail.Outgoing{{Name: "faktura.pdf", MIMEType: "application/pdf", Data: []byte("%PDF"), Size: 4}},
	}
	a.outbox = []*outboxItem{
		{ID: "1", Draft: *d, Error: "server unavailable", Waiting: true, acc: acc1},
		{ID: "2", Draft: protonmail.Draft{Subject: "Jiný účet"}, acc: acc2},
	}
	a.saveOutbox(acc1)
	a.saveOutbox(acc2)

	b := &App{} // a new run of Klient
	b.loadOutbox(acc1)
	if len(b.outbox) != 1 {
		t.Fatalf("loaded %d messages for the first account", len(b.outbox))
	}
	it := b.outbox[0]
	if it.acc != acc1 || it.Draft.Subject != "Faktura" || !it.Waiting || it.Draft.To[0].Address != "jana@example.org" {
		t.Errorf("loaded %+v", it)
	}
	if att := it.Draft.Attachments[0]; att.Name != "faktura.pdf" || string(att.Data) != "%PDF" {
		t.Errorf("attachment %+v", att)
	}
	b.loadOutbox(acc2)
	if len(b.outbox) != 2 || b.outbox[1].acc != acc2 {
		t.Errorf("second account: %d messages", len(b.outbox))
	}
}

func TestNetworkError(t *testing.T) {
	netErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	for _, c := range []struct {
		err  error
		want bool
	}{
		{netErr, true},
		{fmt.Errorf("sending: %w", netErr), true},
		{context.DeadlineExceeded, true},
		{errors.New("550 mailbox unavailable"), false},
		{nil, false},
	} {
		if got := networkError(c.err); got != c.want {
			t.Errorf("networkError(%v) = %v", c.err, got)
		}
	}
}
