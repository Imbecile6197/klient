package ui

import "testing"

func TestParseMailto(t *testing.T) {
	for _, c := range []struct {
		uri, to, subject, cc string
	}{
		{"mailto:jana@example.org", "jana@example.org", "", ""},
		{"mailto:///jana@example.org", "jana@example.org", "", ""}, // as GNOME hands it over
		{"mailto://jana@example.org", "jana@example.org", "", ""},
		{"MAILTO:jana%40example.org?subject=Ahoj%20Jano&cc=petr@example.org", "jana@example.org", "Ahoj Jano", "petr@example.org"},
		{"mailto:///jana@example.org?subject=Faktura", "jana@example.org", "Faktura", ""},
		{"mailto:?to=jana@example.org", "", "", ""},
	} {
		to, q, ok := parseMailto(c.uri)
		if !ok || to != c.to || q.Get("subject") != c.subject || q.Get("cc") != c.cc {
			t.Errorf("%s: to=%q subject=%q cc=%q ok=%v", c.uri, to, q.Get("subject"), q.Get("cc"), ok)
		}
	}
	if _, _, ok := parseMailto("https://example.org"); ok {
		t.Error("not a mailto link accepted")
	}
}
