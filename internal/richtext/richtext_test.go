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
