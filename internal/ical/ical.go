// Package ical reads meeting invitations (iCalendar, RFC 5545) and writes
// the replies to them (iTIP, RFC 5546). Only what a mail client needs: the
// first VEVENT, its organizer and attendees.
package ical

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/i18n"
)

type Attendee struct {
	Email    string
	Name     string
	PartStat string // NEEDS-ACTION, ACCEPTED, DECLINED, TENTATIVE
}

type Event struct {
	Method      string // REQUEST, CANCEL, REPLY, PUBLISH
	UID         string
	Sequence    string
	Summary     string
	Location    string
	Description string
	Start, End  time.Time
	AllDay      bool
	Organizer   Attendee
	Attendees   []Attendee
	// raw DTSTART/DTEND lines, copied verbatim into replies
	rawStart, rawEnd string
}

type prop struct {
	name   string
	params map[string]string
	value  string
	raw    string
}

// unfold joins continuation lines (starting with a space or tab).
func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

func parseLine(l string) (prop, bool) {
	// The value starts at the first ':' that is not inside a quoted param.
	inQ := false
	colon := -1
	for i, r := range l {
		if r == '"' {
			inQ = !inQ
		} else if r == ':' && !inQ {
			colon = i
			break
		}
	}
	if colon < 0 {
		return prop{}, false
	}
	head, value := l[:colon], l[colon+1:]
	parts := splitParams(head)
	p := prop{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: value, raw: l}
	for _, kv := range parts[1:] {
		k, v, _ := strings.Cut(kv, "=")
		p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return p, true
}

func splitParams(s string) []string {
	var out []string
	inQ := false
	start := 0
	for i, r := range s {
		if r == '"' {
			inQ = !inQ
		} else if r == ';' && !inQ {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

func mailto(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
	}
	return v
}

// windowsZones maps the zone names Outlook/Exchange use to IANA names.
var windowsZones = map[string]string{
	"Central Europe Standard Time":   "Europe/Prague",
	"Central European Standard Time": "Europe/Warsaw",
	"W. Europe Standard Time":        "Europe/Berlin",
	"Romance Standard Time":          "Europe/Paris",
	"GMT Standard Time":              "Europe/London",
	"E. Europe Standard Time":        "Europe/Bucharest",
	"FLE Standard Time":              "Europe/Kiev",
	"Russian Standard Time":          "Europe/Moscow",
	"Eastern Standard Time":          "America/New_York",
	"Central Standard Time":          "America/Chicago",
	"Mountain Standard Time":         "America/Denver",
	"Pacific Standard Time":          "America/Los_Angeles",
	"UTC":                            "UTC",
	"Coordinated Universal Time":     "UTC",
	"China Standard Time":            "Asia/Shanghai",
	"Tokyo Standard Time":            "Asia/Tokyo",
	"India Standard Time":            "Asia/Kolkata",
	"AUS Eastern Standard Time":      "Australia/Sydney",
}

func location(tzid string) *time.Location {
	if tzid == "" {
		return time.Local
	}
	if l, err := time.LoadLocation(tzid); err == nil {
		return l
	}
	if n, ok := windowsZones[tzid]; ok {
		if l, err := time.LoadLocation(n); err == nil {
			return l
		}
	}
	return time.Local
}

func parseTime(p prop) (time.Time, bool, error) {
	v := strings.TrimSpace(p.value)
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	t, err := time.ParseInLocation("20060102T150405", v, location(p.params["TZID"]))
	return t, false, err
}

func attendee(p prop) Attendee {
	return Attendee{Email: mailto(p.value), Name: p.params["CN"], PartStat: strings.ToUpper(p.params["PARTSTAT"])}
}

// Parse reads the first event of an iCalendar object.
func Parse(data []byte) (*Event, error) {
	ev := &Event{}
	inEvent, seen := false, false
	depth := 0 // nested components inside VEVENT (VALARM)
	for _, l := range unfold(string(data)) {
		p, ok := parseLine(l)
		if !ok {
			continue
		}
		switch {
		case p.name == "METHOD" && !inEvent:
			ev.Method = strings.ToUpper(strings.TrimSpace(p.value))
			continue
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VEVENT") && !seen:
			inEvent, seen = true, true
			continue
		case p.name == "BEGIN" && inEvent:
			depth++
			continue
		case p.name == "END" && inEvent && depth > 0:
			depth--
			continue
		case p.name == "END" && strings.EqualFold(p.value, "VEVENT"):
			inEvent = false
			continue
		}
		if !inEvent || depth > 0 {
			continue
		}
		switch p.name {
		case "UID":
			ev.UID = p.value
		case "SEQUENCE":
			ev.Sequence = strings.TrimSpace(p.value)
		case "SUMMARY":
			ev.Summary = unescape(p.value)
		case "LOCATION":
			ev.Location = unescape(p.value)
		case "DESCRIPTION":
			ev.Description = unescape(p.value)
		case "DTSTART":
			t, allDay, err := parseTime(p)
			if err != nil {
				return nil, fmt.Errorf(i18n.T("invalid event start: %w"), err)
			}
			ev.Start, ev.AllDay, ev.rawStart = t, allDay, p.raw
		case "DTEND":
			if t, _, err := parseTime(p); err == nil {
				ev.End, ev.rawEnd = t, p.raw
			}
		case "ORGANIZER":
			ev.Organizer = attendee(p)
		case "ATTENDEE":
			ev.Attendees = append(ev.Attendees, attendee(p))
		}
	}
	if !seen {
		return nil, errors.New(i18n.T("the invitation contains no event"))
	}
	if ev.End.IsZero() {
		if ev.AllDay {
			ev.End = ev.Start.AddDate(0, 0, 1)
		} else {
			ev.End = ev.Start.Add(time.Hour)
		}
	}
	return ev, nil
}

// Attendee returns the attendee with the given address.
func (e *Event) Attendee(email string) (Attendee, bool) {
	for _, a := range e.Attendees {
		if strings.EqualFold(a.Email, email) {
			return a, true
		}
	}
	return Attendee{}, false
}

func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`)
	return r.Replace(s)
}

// fold splits lines longer than 75 octets (RFC 5545 3.1).
func fold(line string) string {
	var sb strings.Builder
	n := 0
	for _, r := range line {
		l := len(string(r))
		if n+l > 75 {
			sb.WriteString("\r\n ")
			n = 1
		}
		sb.WriteRune(r)
		n += l
	}
	return sb.String()
}

// Reply builds the iTIP REPLY by which attendee (e-mail, name) answers the
// invitation; partStat is ACCEPTED, TENTATIVE or DECLINED.
func (e *Event) Reply(email, name, partStat string, now time.Time) []byte {
	lines := []string{
		"BEGIN:VCALENDAR",
		"PRODID:-//Klient//Klient//CS",
		"VERSION:2.0",
		"METHOD:REPLY",
		"BEGIN:VEVENT",
		"UID:" + e.UID,
		"DTSTAMP:" + now.UTC().Format("20060102T150405Z"),
	}
	if e.Sequence != "" {
		lines = append(lines, "SEQUENCE:"+e.Sequence)
	}
	if e.rawStart != "" {
		lines = append(lines, e.rawStart)
	}
	if e.rawEnd != "" {
		lines = append(lines, e.rawEnd)
	}
	if e.Summary != "" {
		lines = append(lines, "SUMMARY:"+escape(e.Summary))
	}
	org := "ORGANIZER"
	if e.Organizer.Name != "" {
		org += `;CN="` + e.Organizer.Name + `"`
	}
	lines = append(lines, org+":mailto:"+e.Organizer.Email)
	att := "ATTENDEE;PARTSTAT=" + partStat
	if name != "" {
		att += `;CN="` + strings.ReplaceAll(name, `"`, "") + `"`
	}
	lines = append(lines, att+":mailto:"+email, "END:VEVENT", "END:VCALENDAR")
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(fold(l) + "\r\n")
	}
	return []byte(sb.String())
}
