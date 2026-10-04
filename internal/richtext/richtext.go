// Package richtext converts the composer's styled text into the HTML and
// plain-text bodies of an e-mail.
package richtext

import (
	"html"
	"strings"
)

// Span is a run of text with one style.
type Span struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Link      string // target URL, "" if not a link
	// HTML is inserted as it is into the HTML body (an HTML signature);
	// Text is then its plain-text version.
	HTML string
}

// Bullet starts a list item line; QuotePrefix starts a quoted line.
const (
	Bullet      = "• "
	QuotePrefix = "> "
)

// Formatted reports whether any span carries formatting (so HTML is worth sending).
func Formatted(spans []Span) bool {
	for _, s := range spans {
		if s.Bold || s.Italic || s.Underline || s.Strike || s.Link != "" || s.HTML != "" {
			return true
		}
	}
	for _, line := range strings.Split(Plain(spans), "\n") {
		if strings.HasPrefix(line, Bullet) {
			return true
		}
	}
	return false
}

// Plain returns the text with links written as "text <url>".
func Plain(spans []Span) string {
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.Text)
		if s.Link != "" && s.Link != s.Text {
			sb.WriteString(" <" + s.Link + ">")
		}
	}
	return sb.String()
}

// splitLines splits spans at newlines, keeping styles.
func splitLines(spans []Span) [][]Span {
	lines := [][]Span{{}}
	for _, s := range spans {
		parts := strings.Split(s.Text, "\n")
		for i, p := range parts {
			if i > 0 {
				lines = append(lines, []Span{})
			}
			if p != "" {
				c := s
				c.Text = p
				lines[len(lines)-1] = append(lines[len(lines)-1], c)
			}
		}
	}
	return lines
}

func lineText(line []Span) string {
	var sb strings.Builder
	for _, s := range line {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// trimPrefix removes n leading characters (bytes of an ASCII/UTF-8 prefix).
func trimPrefix(line []Span, prefix string) []Span {
	rest := prefix
	var out []Span
	for _, s := range line {
		if rest != "" {
			if strings.HasPrefix(s.Text, rest) {
				s.Text = s.Text[len(rest):]
				rest = ""
			} else if strings.HasPrefix(rest, s.Text) {
				rest = rest[len(s.Text):]
				continue
			}
		}
		if s.Text != "" {
			out = append(out, s)
		}
	}
	return out
}

func spanHTML(s Span) string {
	t := html.EscapeString(s.Text)
	if s.Bold {
		t = "<b>" + t + "</b>"
	}
	if s.Italic {
		t = "<i>" + t + "</i>"
	}
	if s.Underline {
		t = "<u>" + t + "</u>"
	}
	if s.Strike {
		t = "<s>" + t + "</s>"
	}
	if s.Link != "" {
		t = `<a href="` + html.EscapeString(s.Link) + `">` + t + "</a>"
	}
	return t
}

func inlineHTML(line []Span) string {
	var sb strings.Builder
	for _, s := range line {
		sb.WriteString(spanHTML(s))
	}
	return sb.String()
}

// HTML renders the spans as an e-mail body: paragraphs of lines, "• " lines
// as a bulleted list and "> " lines as a quote.
func HTML(spans []Span) string {
	var sb strings.Builder
	sb.WriteString(`<div style="font-family:sans-serif">`)
	// Raw HTML spans split the text into parts rendered on their own.
	start := 0
	for i, s := range spans {
		if s.HTML != "" {
			writeText(&sb, spans[start:i])
			sb.WriteString(s.HTML)
			start = i + 1
		}
	}
	writeText(&sb, spans[start:])
	sb.WriteString("</div>")
	return sb.String()
}

func writeText(sb *strings.Builder, spans []Span) {
	if len(spans) == 0 {
		return
	}
	lines := splitLines(spans)
	const (
		none = iota
		list
		quote
	)
	mode := none
	closeMode := func() {
		switch mode {
		case list:
			sb.WriteString("</ul>")
		case quote:
			sb.WriteString("</blockquote>")
		}
		mode = none
	}
	for i, line := range lines {
		text := lineText(line)
		switch {
		case strings.HasPrefix(text, Bullet):
			if mode != list {
				closeMode()
				sb.WriteString("<ul>")
				mode = list
			}
			sb.WriteString("<li>" + inlineHTML(trimPrefix(line, Bullet)) + "</li>")
			continue
		case strings.HasPrefix(text, ">"):
			if mode != quote {
				closeMode()
				sb.WriteString(`<blockquote style="margin:0 0 0 .8ex;border-left:2px solid #ccc;padding-left:1ex;color:#555">`)
				mode = quote
			}
			q := trimPrefix(line, ">")
			q = trimPrefix(q, " ")
			sb.WriteString(inlineHTML(q) + "<br>")
			continue
		}
		closeMode()
		sb.WriteString(inlineHTML(line))
		if i < len(lines)-1 {
			sb.WriteString("<br>")
		}
	}
	closeMode()
}
