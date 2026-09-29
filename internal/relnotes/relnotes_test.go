package relnotes

import (
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/internal/update"
)

const sample = "## English\n\nKlient now speaks **English**.\n\n- First `item`\n- Second item\n  continues here\n\n**Install:** `sudo dnf install x`\n\n## Česky\n\nKlient mluví **česky**.\n\n- První bod\n"

func TestParse(t *testing.T) {
	en := Parse(sample, "en")
	if len(en) != 3 || en[0].Bullet || !en[1].Bullet || en[2].Text != "Second item continues here" {
		t.Fatalf("en = %+v", en)
	}
	cs := Parse(sample, "cs")
	if len(cs) != 2 || cs[1].Text != "První bod" {
		t.Fatalf("cs = %+v", cs)
	}
	if got := Markup("a **b** `c` <d>"); got != "a <b>b</b> <tt>c</tt> &lt;d&gt;" {
		t.Errorf("Markup = %q", got)
	}
	if got := AppStream(en); !strings.HasPrefix(got, "<p>Klient now speaks <em>English</em>.</p><ul><li>First <code>item</code></li>") || !strings.HasSuffix(got, "</ul>") {
		t.Errorf("AppStream = %q", got)
	}
	// Plain text without sections (e.g. an old GitHub release).
	if b := Parse("- only\n", "cs"); len(b) != 1 || !b[0].Bullet {
		t.Errorf("no sections: %+v", b)
	}
}

func TestEmbedded(t *testing.T) {
	all := Embedded("cs", "", "99.0.0", update.Newer)
	if len(all) < 2 {
		t.Fatalf("embedded notes: %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if !update.Newer(all[i-1].Version, all[i].Version) {
			t.Errorf("not sorted: %s before %s", all[i-1].Version, all[i].Version)
		}
	}
	some := Embedded("en", "0.6.8", "0.7.0", update.Newer)
	if len(some) != 1 || some[0].Version != "0.7.0" {
		t.Errorf("range: %+v", some)
	}
}
