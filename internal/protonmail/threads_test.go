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
