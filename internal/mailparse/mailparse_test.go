package mailparse

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/internal/i18n"
)

func TestBodyForAIPrefersHTMLWhenPlainIsStub(t *testing.T) {
	plain := "Please view this e-mail in a browser."
	html := `<html><body><p>Dobrý den,</p><p>Vaše pojistka č. 12345 je splatná do 15. 11. Částka 2 450 Kč.</p><p>S pozdravem pojišťovna</p></body></html>`
	got := BodyForAI(plain, html)
	if !strings.Contains(got, "pojistka") || !strings.Contains(got, "2 450") {
		t.Fatalf("want full HTML body, got %q", got)
	}
	if got == plain {
		t.Fatal("kept stub plain part")
	}
}

func TestBodyForAIKeepsGoodPlain(t *testing.T) {
	plain := "Ahoj, tady je delší text zprávy bez HTML stubu, stačí na shrnutí a odpověď."
	html := `<html><body><p>Ahoj, tady je delší text zprávy bez HTML stubu, stačí na shrnutí a odpověď.</p></body></html>`
	if got := BodyForAI(plain, html); got != plain {
		t.Fatalf("got %q", got)
	}
}

func TestHTMLToText(t *testing.T) {
	defer i18n.Set(i18n.Lang())
	i18n.Set("cs")
	src := `<html><head><style>p{color:red}</style><title>x</title></head><body>
<p>Dobrý den,</p><p>klikněte <a href="https://evil.example/login">paypal.com</a> ihned.</p>
<script>alert(1)</script><img src="https://track.example/p.gif" alt="logo"><ul><li>jedna</li><li>dvě</li></ul></body></html>`
	text, links := HTMLToText(src)
	for _, want := range []string{"Dobrý den,", "paypal.com <https://evil.example/login>", "[obrázek: logo]", "• jedna"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, bad := range []string{"alert(1)", "color:red"} {
		if strings.Contains(text, bad) {
			t.Errorf("script/style leaked: %q", bad)
		}
	}
	if len(links) != 1 || links[0] != "https://evil.example/login" {
		t.Errorf("links = %v", links)
	}
}

func TestParseMultipart(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nAhoj, viz https://example.org/doc\r\n" +
		"--XX\r\nContent-Type: application/pdf; name=\"a.pdf\"\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\n\r\n%PDF-1.4\r\n" +
		"--XX--\r\n"
	b := Parse([]byte(raw), "multipart/mixed")
	if !strings.Contains(b.Text, "Ahoj") {
		t.Errorf("text = %q", b.Text)
	}
	if len(b.Attachments) != 1 || b.Attachments[0].Filename != "a.pdf" {
		t.Errorf("attachments = %+v", b.Attachments)
	}
	if len(b.Links) != 1 || b.Links[0] != "https://example.org/doc" {
		t.Errorf("links = %v", b.Links)
	}
}

func TestDisplayAddress(t *testing.T) {
	cases := map[string]*mail.Address{
		"Libor Macák <jsem@libormacak.eu>": {Name: "Libor Macák", Address: "jsem@libormacak.eu"},
		"Libor Macák <x@y.cz>":             {Name: "=?utf-8?q?Libor_Mac=C3=A1k?=", Address: "x@y.cz"},
		`"Novák, Jan" <jan@x.cz>`:          {Name: "Novák, Jan", Address: "jan@x.cz"},
		"solo@x.cz":                        {Address: "solo@x.cz"},
	}
	for want, a := range cases {
		got := DisplayAddress(a)
		if got != want {
			t.Errorf("DisplayAddress(%+v) = %q, want %q", a, got, want)
		}
		back, err := mail.ParseAddress(got)
		if err != nil || back.Address != a.Address {
			t.Errorf("%q does not parse back: %v", got, err)
		}
	}
}
