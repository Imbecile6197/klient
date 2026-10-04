package richtext

import (
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	spans := []Span{
		{Text: "Ahoj "}, {Text: "tučně", Bold: true}, {Text: " a "}, {Text: "web", Link: "https://example.com/?a=1&b=2"},
		{Text: "\n• první\n• "}, {Text: "druhá", Italic: true},
		{Text: "\n\n> citace <x>\n> další"},
		{Text: "\nkonec"},
	}
	got := HTML(spans)
	for _, want := range []string{
		"Ahoj <b>tučně</b> a ",
		`<a href="https://example.com/?a=1&amp;b=2">web</a>`,
		"<ul><li>první</li><li><i>druhá</i></li></ul>",
		"citace &lt;x&gt;<br>další<br></blockquote>",
		"konec</div>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if Plain(spans) != "Ahoj tučně a web <https://example.com/?a=1&b=2>\n• první\n• druhá\n\n> citace <x>\n> další\nkonec" {
		t.Errorf("plain = %q", Plain(spans))
	}
	if !Formatted(spans) || Formatted([]Span{{Text: "jen text\n> citace"}}) {
		t.Error("Formatted wrong")
	}
}

func TestSignature(t *testing.T) {
	page := `<!doctype html><html><head><title>x</title><style>p{}</style></head>
<body style="margin:0"><table onclick="evil()"><tr><td><a href="javascript:alert(1)">A</a>
<a href="https://example.com">web</a></td></tr></table><script>alert(1)</script></body></html>`
	got := CleanSignature(page)
	for _, bad := range []string{"<html", "<body", "<head", "title", "onclick", "javascript:", "<script"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q left in %s", bad, got)
		}
	}
	if !strings.HasPrefix(got, "<table>") || !strings.Contains(got, `<a href="https://example.com">web</a>`) {
		t.Errorf("signature content lost: %s", got)
	}

	spans := []Span{{Text: "Ahoj\n\n"}, {Text: "-- \nJan", HTML: SignatureHTML("<b>Jan</b>")}, {Text: "\n\n> citace"}}
	h := HTML(spans)
	if !strings.Contains(h, `Ahoj<br><br><div class="klient-signature" style="margin-top:4px"><b>Jan</b></div><br><br><blockquote`) {
		t.Errorf("signature in HTML: %s", h)
	}
	if Plain(spans) != "Ahoj\n\n-- \nJan\n\n> citace" || !Formatted(spans[1:2]) {
		t.Errorf("plain = %q", Plain(spans))
	}
}
