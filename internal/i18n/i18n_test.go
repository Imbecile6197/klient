package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Imbecile6197/klient/po"
)

func TestPlurals(t *testing.T) {
	for n, want := range map[int]int{0: 2, 1: 0, 2: 1, 4: 1, 5: 2, 11: 2, 22: 2} {
		if got := pluralIndex("cs", n); got != want {
			t.Errorf("cs plural(%d) = %d, want %d", n, got, want)
		}
	}
	if pluralIndex("en", 1) != 0 || pluralIndex("en", 0) != 1 || pluralIndex("en", 7) != 1 {
		t.Error("en plural forms")
	}
}

func TestParse(t *testing.T) {
	cat := parse(`msgid ""
msgstr "Language: cs\n"

#, fuzzy
msgid "Skipped"
msgstr "Přeskočeno"

msgctxt "action"
msgid "Archive"
msgstr "Archivovat"

msgid "Multi"
"line"
msgstr ""
"Víc"
"řádků\n"

msgid "%d file"
msgid_plural "%d files"
msgstr[0] "%d soubor"
msgstr[1] "%d soubory"
msgstr[2] "%d souborů"
`)
	if _, ok := cat[key{"", "Skipped"}]; ok {
		t.Error("fuzzy entry used")
	}
	if got := cat[key{"action", "Archive"}]; len(got) != 1 || got[0] != "Archivovat" {
		t.Errorf("context entry: %q", got)
	}
	if got := cat[key{"", "Multiline"}]; len(got) != 1 || got[0] != "Víc"+"řádků\n" {
		t.Errorf("multi-line entry: %q", got)
	}
	if got := cat[key{"", "%d file"}]; len(got) != 3 || got[2] != "%d souborů" {
		t.Errorf("plural entry: %q", got)
	}
}

func TestSetAndLookup(t *testing.T) {
	defer Set(Lang())
	Set("cs")
	if T("Inbox") != "Doručená pošta" || C("action", "Archive") != "Archivovat" {
		t.Fatalf("cs lookup: %q %q", T("Inbox"), C("action", "Archive"))
	}
	if got := N("%d unread message", "%d unread messages", 3); got != "%d nepřečtené zprávy" {
		t.Fatalf("cs plural: %q", got)
	}
	Set("en")
	if T("Inbox") != "Inbox" || N("%d unread message", "%d unread messages", 3) != "%d unread messages" {
		t.Fatal("en falls back to the msgid")
	}
	if normalize("cs_CZ.UTF-8") != "cs" || normalize("de_DE") != "en" {
		t.Fatal("normalize")
	}
}

func TestApplyConfigLanguage(t *testing.T) {
	defer Set(Lang())
	Apply("cs")
	if Lang() != "cs" {
		t.Fatalf("Apply(cs) = %q", Lang())
	}
	Apply("en")
	if Lang() != "en" {
		t.Fatalf("Apply(en) = %q", Lang())
	}
	// Empty = system; KLIENT_LANGUAGE wins over OS.
	t.Setenv("KLIENT_LANGUAGE", "cs")
	Apply("")
	if Lang() != "cs" {
		t.Fatalf("Apply(\"\") with KLIENT_LANGUAGE=cs = %q", Lang())
	}
	t.Setenv("KLIENT_LANGUAGE", "en")
	Apply("")
	if Lang() != "en" {
		t.Fatalf("Apply(\"\") with KLIENT_LANGUAGE=en = %q", Lang())
	}
}

func TestPOSIXLocale(t *testing.T) {
	for _, v := range []string{"C", "POSIX", "C.UTF-8", "c.utf8", "C.utf8", "  C.UTF-8  "} {
		if !isPOSIXLocale(v) {
			t.Errorf("%q should be ignored as a UI language", v)
		}
	}
	for _, v := range []string{"cs_CZ.UTF-8", "en_US", "en"} {
		if isPOSIXLocale(v) {
			t.Errorf("%q should not be treated as POSIX/C", v)
		}
	}
}

func TestDetectSystemIgnoresCUTF8(t *testing.T) {
	defer Set(Lang())
	t.Setenv("KLIENT_LANGUAGE", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "C.UTF-8")
	got := detectSystem()
	// Must not treat C.UTF-8 as English; fall through to platform / "en".
	// On this Mac with Czech AppleLanguages, platformLang returns "cs".
	if got == "en" && platformLang() == "cs" {
		t.Fatalf("LANG=C.UTF-8 made detectSystem return en; platformLang=%q", platformLang())
	}
	if pl := platformLang(); pl != "" && got != pl && got != "en" {
		t.Fatalf("detectSystem=%q platformLang=%q", got, pl)
	}
	Apply("")
	if pl := platformLang(); pl == "cs" && Lang() != "cs" {
		t.Fatalf("Apply(\"\") with LANG=C.UTF-8 → %q, want cs from AppleLanguages", Lang())
	}
}

var verbs = regexp.MustCompile(`%[-+# 0]*[0-9]*(?:\.[0-9]+)?[vTtbcdoOqxXUeEfFgGsqpw%]`)

func verbList(s string) []string {
	v := verbs.FindAllString(s, -1)
	// %s and %v are interchangeable; only the count and kind of the rest matter.
	for i := range v {
		v[i] = strings.NewReplacer("%v", "%s").Replace(v[i])
	}
	return v
}

// TestCatalogComplete finds every i18n.T/C/N call in the code and checks
// that the Czech catalog translates it with the same format verbs.
func TestCatalogComplete(t *testing.T) {
	b, err := po.Files.ReadFile("cs.po")
	if err != nil {
		t.Fatal(err)
	}
	cat := parse(string(b))
	used := map[key]bool{}
	fset := token.NewFileSet()
	root := filepath.Join("..", "..")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "third_party" || d.Name() == "build" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "i18n" {
				return true
			}
			str := func(i int) (string, bool) {
				if i >= len(call.Args) {
					return "", false
				}
				bl, ok := call.Args[i].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return "", false
				}
				s, err := strconv.Unquote(bl.Value)
				return s, err == nil
			}
			pos := fset.Position(call.Pos())
			var k key
			var forms int
			switch sel.Sel.Name {
			case "T":
				id, ok := str(0)
				if !ok {
					t.Errorf("%s: i18n.T needs a string literal", pos)
					return true
				}
				k, forms = key{"", id}, 1
			case "C":
				ctx, ok1 := str(0)
				id, ok2 := str(1)
				if !ok1 || !ok2 {
					t.Errorf("%s: i18n.C needs string literals", pos)
					return true
				}
				k, forms = key{ctx, id}, 1
			case "N":
				one, ok1 := str(0)
				other, ok2 := str(1)
				if !ok1 || !ok2 {
					t.Errorf("%s: i18n.N needs string literals", pos)
					return true
				}
				if !slices.Equal(verbList(one), verbList(other)) {
					t.Errorf("%s: singular and plural have different verbs", pos)
				}
				k, forms = key{"", one}, 3
			default:
				return true
			}
			used[k] = true
			tr := cat[k]
			if len(tr) != forms {
				t.Errorf("%s: %q (context %q) has %d Czech forms, want %d", pos, k.id, k.ctx, len(tr), forms)
				return true
			}
			for _, s := range tr {
				if s == "" {
					t.Errorf("%s: %q has an empty translation", pos, k.id)
				}
				if !slices.Equal(verbList(s), verbList(k.id)) {
					t.Errorf("%s: %q → %q: format verbs differ", pos, k.id, s)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for k := range cat {
		if k.id != "" && !used[k] {
			t.Errorf("unused translation: %q (context %q)", k.id, k.ctx)
		}
	}
}
