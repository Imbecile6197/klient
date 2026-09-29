// Package relnotes reads release notes: the Markdown files embedded from
// packaging/release-notes, or the body of a GitHub release. A file has an
// "## English" and an "## Česky" section with paragraphs and "- " bullets;
// inline **bold** and `code` are supported.
package relnotes

import (
	"html"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	releasenotes "github.com/Imbecile6197/klient/packaging/release-notes"
)

// Block is a paragraph or a bullet point (inline Markdown).
type Block struct {
	Bullet bool
	Text   string
}

// Release is the notes of one version.
type Release struct {
	Version string
	Blocks  []Block
}

var sectionNames = map[string]string{"english": "en", "česky": "cs", "cesky": "cs", "čeština": "cs"}

// Parse returns the blocks of the section in lang ("en", "cs"); text without
// sections is taken whole. The installation hint is left out: it is for
// the GitHub page, not for a running Klient.
func Parse(md, lang string) []Block {
	sections := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			cur = sectionNames[strings.ToLower(strings.TrimSpace(line[3:]))]
			continue
		}
		sections[cur] = append(sections[cur], line)
	}
	lines, ok := sections[lang]
	if !ok {
		if lines, ok = sections["en"]; !ok {
			lines = sections[""]
		}
	}
	var out []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, Block{Text: strings.Join(para, " ")})
			para = nil
		}
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			flush()
		case strings.HasPrefix(t, "**Install:**"), strings.HasPrefix(t, "**Instalace:**"):
			flush()
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
			flush()
			out = append(out, Block{Bullet: true, Text: strings.TrimSpace(t[2:])})
		case len(out) > 0 && out[len(out)-1].Bullet && len(para) == 0 && strings.HasPrefix(line, "  "):
			// Continuation of a bullet.
			out[len(out)-1].Text += " " + t
		default:
			para = append(para, t)
		}
	}
	flush()
	return out
}

var (
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	codeRe = regexp.MustCompile("`([^`]+)`")
)

// Markup converts inline Markdown to Pango markup.
func Markup(s string) string {
	s = html.EscapeString(s)
	s = codeRe.ReplaceAllString(s, "<tt>$1</tt>")
	return boldRe.ReplaceAllString(s, "<b>$1</b>")
}

// AppStream renders blocks as the AppStream description markup that
// AdwAboutDialog shows under "What's New" (p, ul, li, em, code).
func AppStream(blocks []Block) string {
	var sb strings.Builder
	inList := false
	for _, b := range blocks {
		t := html.EscapeString(b.Text)
		t = codeRe.ReplaceAllString(t, "<code>$1</code>")
		t = boldRe.ReplaceAllString(t, "<em>$1</em>")
		if b.Bullet && !inList {
			sb.WriteString("<ul>")
			inList = true
		}
		if !b.Bullet && inList {
			sb.WriteString("</ul>")
			inList = false
		}
		if b.Bullet {
			sb.WriteString("<li>" + t + "</li>")
		} else {
			sb.WriteString("<p>" + t + "</p>")
		}
	}
	if inList {
		sb.WriteString("</ul>")
	}
	return sb.String()
}

// Embedded returns the notes of the versions after `after` up to and
// including `upto` (newest first). An empty `after` means no lower bound.
func Embedded(lang, after, upto string, newer func(a, b string) bool) []Release {
	var out []Release
	_ = fs.WalkDir(releasenotes.Files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(name, ".md") {
			return err
		}
		v := strings.TrimSuffix(name, ".md")
		if (after != "" && !newer(v, after)) || newer(v, upto) {
			return nil
		}
		b, err := releasenotes.Files.ReadFile(name)
		if err != nil {
			return err
		}
		if blocks := Parse(string(b), lang); len(blocks) > 0 {
			out = append(out, Release{Version: v, Blocks: blocks})
		}
		return nil
	})
	slices.SortFunc(out, func(x, y Release) int {
		switch {
		case newer(x.Version, y.Version):
			return -1
		case newer(y.Version, x.Version):
			return 1
		}
		return 0
	})
	return out
}
