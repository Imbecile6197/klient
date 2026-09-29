package ui

import (
	"testing"
	"time"

	"github.com/libormacak/klient/internal/protonmail"
)

func TestMatchContacts(t *testing.T) {
	all := []protonmail.Contact{
		{Name: "Jan Novák", Email: "jan@example.cz"},
		{Name: "Libor Macák", Email: "jsem@libormacak.eu"},
		{Name: "", Email: "info@macak-shop.cz", Recent: true},
	}
	got := matchContacts(all, "macak", 10)
	if len(got) != 2 || got[0].Name != "Libor Macák" {
		t.Errorf("diacritics-insensitive match failed: %+v", got)
	}
	if got := matchContacts(all, "NOV", 10); len(got) != 1 || got[0].Email != "jan@example.cz" {
		t.Errorf("case-insensitive match failed: %+v", got)
	}
	if got := matchContacts(all, "a", 1); len(got) != 1 {
		t.Errorf("limit not applied: %d", len(got))
	}
}

func TestCountdownTexts(t *testing.T) {
	now := time.Now()
	cases := map[time.Duration]string{
		30 * time.Second:                             "za 29 s",
		10*time.Minute + 30*time.Second:              "za 10 min",
		2*time.Hour + 5*time.Minute + 30*time.Second: "za 2 h 5 min",
		3*24*time.Hour + time.Hour:                   "za 3 dny",
		10*24*time.Hour + time.Hour:                  "za 10 dní",
	}
	for d, want := range cases {
		if got := countdown(now.Add(d)); got != want && !(d < time.Minute && got == "za 30 s") {
			t.Errorf("countdown(%v) = %q, want %q", d, got, want)
		}
	}
	if countdown(now.Add(-time.Second)) != "právě teď" {
		t.Error("past time")
	}
	if got := unreadText(3); got != "3 nepřečtené zprávy" {
		t.Error(got)
	}
	tomorrow := at(now.AddDate(0, 0, 1), 8)
	if got := formatWhen(tomorrow); got != "zítra v 8:00" {
		t.Errorf("formatWhen = %q", got)
	}
	if nm := nextMonday(now); nm.Weekday() != time.Monday || !nm.After(now) {
		t.Errorf("nextMonday = %v", nm)
	}
}
