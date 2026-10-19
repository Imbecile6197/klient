package ui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/mailbox"
	"github.com/Imbecile6197/klient/internal/protonmail"
)

type listAccount struct {
	mailbox.Account
	msgs  []protonmail.Summary // newest first
	pages int
}

func (l *listAccount) List(_ context.Context, _ string, page, size int) ([]protonmail.Summary, error) {
	l.pages++
	from := min(page*size, len(l.msgs))
	return l.msgs[from:min(from+size, len(l.msgs))], nil
}

// Unread unjudged mail beyond the first page is found too, judged and old
// mail is not, and reading stops at mail older than catchUpMax.
func TestUnjudgedPages(t *testing.T) {
	now := time.Now()
	acc := &listAccount{}
	for i := range 130 { // one per hour, newest first
		m := protonmail.Summary{}
		m.ID = fmt.Sprint(i)
		m.Time = now.Add(-time.Duration(i) * time.Hour).Unix()
		m.LabelIDs = []string{protonmail.InboxID}
		m.Unread = i == 3 || i == 70 || i == 120 || i == 125
		acc.msgs = append(acc.msgs, m)
	}
	known := func(id string) bool { return id == "3" }
	got := unjudged(context.Background(), acc, known, now, catchUpPages)
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	// 70 is on the second page, 120 and 125 on the third; 3 is judged.
	want := []string{"70", "120", "125"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	if acc.pages != 3 {
		t.Fatalf("read %d pages, want 3", acc.pages)
	}

	// Mail older than a week ends the reading: 300 messages an hour apart,
	// the one 168 hours old is on the fourth page.
	week := &listAccount{}
	for i := range 300 {
		m := protonmail.Summary{}
		m.ID = fmt.Sprint(i)
		m.Time = now.Add(-time.Duration(i) * time.Hour).Unix()
		m.LabelIDs = []string{protonmail.InboxID}
		m.Unread = i == 250 // too old to be checked
		week.msgs = append(week.msgs, m)
	}
	if got := unjudged(context.Background(), week, known, now, catchUpPages); len(got) != 0 || week.pages != 4 {
		t.Fatalf("got %d messages from %d pages, want 0 from 4", len(got), week.pages)
	}

	// The sweep reads at most sweepPages pages.
	long := &listAccount{}
	for i := range 1000 {
		m := protonmail.Summary{}
		m.ID = fmt.Sprint(i)
		m.Time = now.Add(-time.Duration(i) * time.Minute).Unix()
		m.LabelIDs = []string{protonmail.InboxID}
		long.msgs = append(long.msgs, m)
	}
	unjudged(context.Background(), long, known, now, sweepPages)
	if long.pages != sweepPages {
		t.Fatalf("sweep read %d pages, want %d", long.pages, sweepPages)
	}
}
