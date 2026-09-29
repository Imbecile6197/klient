package ical

import (
	"strings"
	"testing"
	"time"
)

const invite = "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nBEGIN:VTIMEZONE\r\nTZID:Central Europe Standard Time\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\nUID:abc-123\r\nSEQUENCE:2\r\nSUMMARY:Porada\\, týden 40\r\n" +
	"DTSTART;TZID=Central Europe Standard Time:20260930T100000\r\n" +
	"DTEND;TZID=Central Europe Standard Time:20260930T113000\r\n" +
	"LOCATION:Zasedačka\r\nORGANIZER;CN=\"Jana: Nová\":mailto:jana@example.com\r\n" +
	"ATTENDEE;CN=Libor;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:jsem@libormacak.eu\r\n" +
	"DESCRIPTION:Program:\\n1. rozpoč\r\n et\r\n" +
	"BEGIN:VALARM\r\nDESCRIPTION:alarm\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestParse(t *testing.T) {
	ev, err := Parse([]byte(invite))
	if err != nil {
		t.Fatal(err)
	}
	prague, _ := time.LoadLocation("Europe/Prague")
	if ev.Method != "REQUEST" || ev.UID != "abc-123" || ev.Summary != "Porada, týden 40" {
		t.Fatalf("%+v", ev)
	}
	if !ev.Start.Equal(time.Date(2026, 9, 30, 10, 0, 0, 0, prague)) || ev.End.Sub(ev.Start) != 90*time.Minute {
		t.Fatalf("time %v %v", ev.Start, ev.End)
	}
	if ev.Organizer.Email != "jana@example.com" || ev.Organizer.Name != "Jana: Nová" {
		t.Fatalf("organizer %+v", ev.Organizer)
	}
	if ev.Description != "Program:\n1. rozpočet" {
		t.Fatalf("description %q", ev.Description)
	}
	if a, ok := ev.Attendee("JSEM@libormacak.eu"); !ok || a.PartStat != "NEEDS-ACTION" {
		t.Fatalf("attendee %+v", a)
	}
	r := string(ev.Reply("jsem@libormacak.eu", "Libor", "ACCEPTED", time.Unix(0, 0)))
	for _, want := range []string{"METHOD:REPLY", "UID:abc-123", "SEQUENCE:2", "ATTENDEE;PARTSTAT=ACCEPTED", "mailto:jana@example.com", "DTSTART;TZID=Central Europe Standard Time:20260930T100000"} {
		if !strings.Contains(strings.ReplaceAll(r, "\r\n ", ""), want) {
			t.Errorf("reply lacks %q:\n%s", want, r)
		}
	}
	if _, err := Parse([]byte("BEGIN:VCALENDAR\nEND:VCALENDAR")); err == nil {
		t.Error("expected error without event")
	}
}

func TestAllDay(t *testing.T) {
	ev, err := Parse([]byte("BEGIN:VEVENT\nDTSTART;VALUE=DATE:20261224\nSUMMARY:Vánoce\nEND:VEVENT"))
	if err != nil || !ev.AllDay || ev.End.Sub(ev.Start) != 24*time.Hour {
		t.Fatalf("%+v %v", ev, err)
	}
}
