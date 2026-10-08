package imapmail

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/Imbecile6197/klient/internal/protonmail"
)

func TestFolderManagement(t *testing.T) {
	set, u, _ := startServers(t)
	ctx := context.Background()
	acc, err := Open(ctx, set, "tajne")
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Close()
	find := func(name string) (protonmail.UserLabel, bool) {
		labels, _ := acc.UserLabels(ctx)
		for _, l := range labels {
			if l.Name == name {
				return l, true
			}
		}
		return protonmail.UserLabel{}, false
	}

	if err := acc.CreateFolder(ctx, "Projekty/2026", false); err != nil {
		t.Fatal(err)
	}
	f, ok := find("Projekty/2026")
	if !ok {
		t.Fatal("created folder not listed")
	}
	if err := acc.RenameFolder(ctx, f.ID, "Projekty/Hotové"); err != nil {
		t.Fatal(err)
	}
	if _, ok := find("Projekty/2026"); ok {
		t.Error("old name still listed")
	}
	g, ok := find("Projekty/Hotové")
	if !ok {
		t.Fatal("renamed folder not listed")
	}
	if err := acc.DeleteFolder(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := find("Projekty/Hotové"); ok {
		t.Error("deleted folder still listed")
	}
	if err := acc.RenameFolder(ctx, protonmail.InboxID, "x"); err == nil {
		t.Error("a system folder was renamed")
	}

	// Trash: two old messages and one recent; first only the old ones go.
	put := func(age time.Duration) {
		if _, err := u.Append("Trash", literal{bytes.NewReader([]byte(hello))}, &imap.AppendOptions{Time: time.Now().Add(-age)}); err != nil {
			t.Fatal(err)
		}
	}
	put(60 * 24 * time.Hour)
	put(45 * 24 * time.Hour)
	put(time.Hour)
	var seen [][2]int
	n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Now().Add(-30*24*time.Hour), func(d, t int) { seen = append(seen, [2]int{d, t}) })
	if err != nil || n != 2 {
		t.Fatalf("older than 30 days: %d, %v", n, err)
	}
	if len(seen) != 2 || seen[0] != [2]int{0, 2} || seen[1] != [2]int{2, 2} {
		t.Errorf("progress %v", seen)
	}
	left, _ := acc.List(ctx, protonmail.TrashID, 0, 50)
	if len(left) != 1 {
		t.Fatalf("%d messages left in Trash, want 1", len(left))
	}
	if n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Time{}, nil); err != nil || n != 1 {
		t.Fatalf("empty: %d, %v", n, err)
	}
	if _, err := acc.EmptyFolder(ctx, protonmail.InboxID, time.Time{}, nil); err == nil {
		t.Error("the inbox was emptied")
	}
	// Automatic emptying of an empty folder is no error ("Bad MSN" from
	// servers that refuse "1:*" on an empty mailbox).
	if n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Now().Add(-30*24*time.Hour), nil); err != nil || n != 0 {
		t.Fatalf("empty Trash again: %d, %v", n, err)
	}
	if n, err := acc.EmptyFolder(ctx, protonmail.SpamID, time.Now().Add(-30*24*time.Hour), nil); err != nil || n != 0 {
		t.Fatalf("empty Spam: %d, %v", n, err)
	}
	// Only recent mail: nothing old enough.
	put(time.Hour)
	if n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Now().Add(-30*24*time.Hour), nil); err != nil || n != 0 {
		t.Fatalf("nothing old enough: %d, %v", n, err)
	}
}

// Klient started offline with an old copy of the folders (no Drafts): saving
// a draft connects, reads the folders again and succeeds; the watcher going
// online tells the window.
func TestBackOnline(t *testing.T) {
	set, _, _ := startServers(t)
	ctx := context.Background()
	a := &Account{set: set, password: "tajne", offline: true,
		roles: map[string]string{protonmail.InboxID: "INBOX"}}
	defer a.Close()
	if a.HasFolder(protonmail.DraftsID) {
		t.Fatal("the stale copy already has Drafts")
	}
	d := &protonmail.Draft{Subject: "Koncept", Body: "text", To: nil}
	if err := a.SaveDraft(ctx, d); err != nil {
		t.Fatalf("SaveDraft after coming online: %v", err)
	}
	if a.StartedOffline() || !a.HasFolder(protonmail.DraftsID) || d.ID == "" {
		t.Fatalf("offline=%v drafts=%v id=%q", a.StartedOffline(), a.HasFolder(protonmail.DraftsID), d.ID)
	}

	b := &Account{set: set, password: "tajne", offline: true, roles: map[string]string{protonmail.InboxID: "INBOX"}}
	defer b.Close()
	changed := make(chan struct{}, 4)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = b.Events(cctx, func(protonmail.Summary) {}, func() { changed <- struct{}{} })
	select {
	case <-changed:
	case <-time.After(10 * time.Second):
		t.Fatal("the window was not told that the account is online")
	}
	if b.StartedOffline() || !b.HasFolder(protonmail.TrashID) {
		t.Error("the watcher did not bring the account online")
	}
}
