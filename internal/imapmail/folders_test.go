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
	n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("older than 30 days: %d, %v", n, err)
	}
	left, _ := acc.List(ctx, protonmail.TrashID, 0, 50)
	if len(left) != 1 {
		t.Fatalf("%d messages left in Trash, want 1", len(left))
	}
	if n, err := acc.EmptyFolder(ctx, protonmail.TrashID, time.Time{}); err != nil || n != 1 {
		t.Fatalf("empty: %d, %v", n, err)
	}
	if _, err := acc.EmptyFolder(ctx, protonmail.InboxID, time.Time{}); err == nil {
		t.Error("the inbox was emptied")
	}
}
