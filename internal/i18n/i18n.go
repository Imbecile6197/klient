// Package i18n translates the user interface. The strings in the code are
// English; translations come from the gettext catalogs in po/, embedded into
// the binary. The language follows the system (LC_ALL, LC_MESSAGES, LANG)
// unless the user picks one in the preferences.
package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Imbecile6197/klient/po"
)

// Languages the interface is available in.
var Languages = []struct{ Code, Name string }{
	{"en", "English"},
	{"cs", "Čeština"},
}

var (
	mu      sync.RWMutex
	lang    = "en"
	catalog map[key][]string
)

type key struct{ ctx, id string }

func init() { Set(detect()) }

// detect picks the language: the "language" setting of Klient, then the
// usual locale variables. Anything that is not Czech gets English.
func detect() string {
	if l := os.Getenv("KLIENT_LANGUAGE"); l != "" {
		return normalize(l)
	}
	if l := configured(); l != "" {
		return normalize(l)
	}
	for _, v := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		val := os.Getenv(v)
		if v == "LANGUAGE" && val != "" {
			// A priority list such as "cs:en".
			val = strings.Split(val, ":")[0]
		}
		if val != "" && val != "C" && val != "POSIX" {
			return normalize(val)
		}
	}
	return "en"
}

func normalize(l string) string {
	l = strings.ToLower(l)
	for _, x := range Languages {
		if strings.HasPrefix(l, x.Code) {
			return x.Code
		}
	}
	return "en"
}

// configured reads the "language" setting straight from the config file
// (the config package itself uses translated strings, so it cannot be
// imported here).
func configured() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "klient", "config.json"))
	if err != nil {
		return ""
	}
	var c struct {
		Language string `json:"language"`
	}
	_ = json.Unmarshal(b, &c)
	return c.Language
}

// Set switches the language ("en", "cs"). Strings already shown keep their
// language until they are built again, so the UI asks for a restart.
func Set(l string) {
	l = normalize(l)
	var cat map[key][]string
	if l != "en" {
		if b, err := po.Files.ReadFile(l + ".po"); err == nil {
			cat = parse(string(b))
		}
	}
	mu.Lock()
	lang, catalog = l, cat
	mu.Unlock()
}

// Lang is the current language code.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// LanguageName is the English name of the current language, for telling an
// AI model which language to answer in.
func LanguageName() string {
	if Lang() == "cs" {
		return "Czech"
	}
	return "English"
}

func lookup(ctx, id string) []string {
	mu.RLock()
	defer mu.RUnlock()
	return catalog[key{ctx, id}]
}

// T translates a string.
func T(msgid string) string {
	if s := lookup("", msgid); len(s) > 0 && s[0] != "" {
		return s[0]
	}
	return msgid
}

// C translates a string with a context that tells apart equal English
// words with different translations (e.g. "Spam" the folder and the verdict).
func C(ctx, msgid string) string {
	if s := lookup(ctx, msgid); len(s) > 0 && s[0] != "" {
		return s[0]
	}
	return msgid
}

// N translates a string with a number: English has two forms, Czech three
// ("1 zpráva", "2 zprávy", "5 zpráv"). The result is usually a format with %d.
func N(singular, plural string, n int) string {
	forms := lookup("", singular)
	i := pluralIndex(Lang(), n)
	if i < len(forms) && forms[i] != "" {
		return forms[i]
	}
	if n == 1 {
		return singular
	}
	return plural
}

// pluralIndex follows the Plural-Forms of each language.
func pluralIndex(l string, n int) int {
	switch l {
	case "cs":
		switch {
		case n == 1:
			return 0
		case n >= 2 && n <= 4:
			return 1
		}
		return 2
	}
	if n == 1 {
		return 0
	}
	return 1
}

// parse reads a .po file: msgctxt, msgid, msgid_plural, msgstr and
// msgstr[n], with multi-line strings. Fuzzy entries are skipped.
func parse(src string) map[key][]string {
	out := map[key][]string{}
	var (
		ctx, id  string
		strs     []string
		fuzzy    bool
		cur      *string
		haveID   bool
		inHeader bool
	)
	flush := func() {
		if haveID && !fuzzy && id != "" && len(strs) > 0 {
			out[key{ctx, id}] = strs
		}
		ctx, id, strs, fuzzy, cur, haveID, inHeader = "", "", nil, false, nil, false, false
	}
	_ = inHeader
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#,"):
			if strings.Contains(line, "fuzzy") {
				fuzzy = true
			}
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "msgctxt "):
			if haveID {
				flush()
			}
			ctx = unquote(line[len("msgctxt "):])
			cur = &ctx
		case strings.HasPrefix(line, "msgid_plural "):
			// The English plural is in the code; only the translations matter.
			cur = nil
		case strings.HasPrefix(line, "msgid "):
			if haveID {
				flush()
			}
			haveID = true
			id = unquote(line[len("msgid "):])
			cur = &id
		case strings.HasPrefix(line, "msgstr["):
			if i := strings.Index(line, "] "); i > 0 {
				strs = append(strs, unquote(line[i+2:]))
				cur = &strs[len(strs)-1]
			}
		case strings.HasPrefix(line, "msgstr "):
			strs = append(strs, unquote(line[len("msgstr "):]))
			cur = &strs[len(strs)-1]
		case strings.HasPrefix(line, `"`) && cur != nil:
			*cur += unquote(line)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return ""
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"', '\\':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
