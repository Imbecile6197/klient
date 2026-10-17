package protonmail

import (
	"testing"

	"github.com/ProtonMail/go-proton-api"
)

func TestGroupThreads(t *testing.T) {
	msg := func(id, conv string, unread bool) Summary {
		return Summary{ID: id, ConversationID: conv, Unread: proton.Bool(unread)}
	}
	// newest first, as the API returns them
	in := []Summary{msg("5", "A", false), msg("4", "B", true), msg("3", "A", true), msg("2", "", false), msg("1", "B", false)}
	got := GroupThreads(in)
	if len(got) != 3 {
		t.Fatalf("want 3 threads, got %d", len(got))
	}
	if got[0].Latest.ID != "5" || len(got[0].Messages) != 2 || !got[0].Unread() {
		t.Errorf("thread A wrong: %+v", got[0])
	}
	if got[1].Latest.ID != "4" || len(got[1].Messages) != 2 {
		t.Errorf("thread B wrong: %+v", got[1])
	}
	if got[2].Latest.ID != "2" || len(got[2].Messages) != 1 {
		t.Errorf("message without conversation should stand alone: %+v", got[2])
	}
}

func TestUniqueCopies(t *testing.T) {
	m := func(id, extID string, unread bool, labels ...string) Summary {
		s := Summary{ID: id, ExternalID: extID, Unread: proton.Bool(unread), LabelIDs: labels}
		return s
	}
	// The same mail delivered to two addresses: one copy archived and
	// unread, the other in the inbox and read.
	in := []Summary{
		m("a1", "x@ex", true, ArchiveID),
		m("a2", "x@ex", false, InboxID),
		m("b", "y@ex", false, InboxID),
		m("c", "", false, InboxID),
		m("d", "", false, InboxID),
	}
	kept, hidden := uniqueCopies(in)
	if len(kept) != 4 {
		t.Fatalf("kept %d: %+v", len(kept), kept)
	}
	if kept[0].ID != "a2" || !bool(kept[0].Unread) {
		t.Errorf("the inbox copy should be shown, unread because of the other: %+v", kept[0])
	}
	if len(hidden["a2"]) != 1 || hidden["a2"][0] != "a1" {
		t.Errorf("hidden %v", hidden)
	}
	if kept[1].ID != "b" || kept[2].ID != "c" || kept[3].ID != "d" {
		t.Errorf("order or messages without Message-ID lost: %+v", kept)
	}

	a := &Account{}
	a.rememberCopies(hidden)
	got := a.withCopies([]string{"a2", "b"})
	if len(got) != 3 || got[2] != "a1" {
		t.Errorf("withCopies = %v", got)
	}
	if got := a.withCopies([]string{"b"}); len(got) != 1 {
		t.Errorf("no copies: %v", got)
	}
}
