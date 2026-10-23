package relnotes

import (
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/docs"
	"github.com/Imbecile6197/klient/internal/i18n"
	"github.com/Imbecile6197/klient/internal/update"
)

func TestLanguageCSPath(t *testing.T) {
	defer i18n.Set(i18n.Lang())
	i18n.Apply("cs")
	if i18n.Lang() != "cs" {
		t.Fatalf("Apply(cs) → %q", i18n.Lang())
	}
	if docs.Page(i18n.Lang()) != "index.cs.html" {
		t.Fatalf("help page: %s", docs.Page(i18n.Lang()))
	}
	rels := Embedded(i18n.Lang(), "", "0.11.6", update.Newer)
	if len(rels) == 0 || len(rels[0].Blocks) == 0 {
		t.Fatal("no embedded notes")
	}
	if !strings.Contains(rels[0].Blocks[0].Text, "Účty") {
		t.Fatalf("want Czech notes, got %q", rels[0].Blocks[0].Text)
	}
	i18n.Apply("en")
	rels = Embedded(i18n.Lang(), "", "0.11.6", update.Newer)
	if !strings.Contains(rels[0].Blocks[0].Text, "IMAP accounts") {
		t.Fatalf("want English notes, got %q", rels[0].Blocks[0].Text)
	}
}
