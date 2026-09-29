// Package mailparse turns decrypted message bodies into plain text for
// display and classification. HTML is never rendered: it is converted to
// text, so remote content (tracking pixels) is never loaded.
package mailparse

import (
	"bytes"
	"io"
	"mime"
	"regexp"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

type Part struct {
	Filename  string
	MIMEType  string
	ContentID string // inline images referenced as cid: from HTML
	Data      []byte
}

type Body struct {
	Text        string
	HTML        string   // original HTML part, "" for plain-text mail
	Links       []string // every http(s) link found in text or HTML hrefs
	Attachments []Part   // only for multipart bodies (PGP/MIME)
}

// Parse handles the three body types Proton returns: text/plain, text/html
// and multipart/mixed (PGP/MIME messages from external senders).
func Parse(body []byte, mimeType string) Body {
	mt, _, _ := mime.ParseMediaType(mimeType)
	switch {
	case strings.HasPrefix(mt, "multipart/"):
		return parseMIME(body)
	case mt == "text/html":
		text, links := HTMLToText(string(body))
		return Body{Text: text, HTML: string(body), Links: dedup(append(links, FindLinks(text)...))}
	default:
		s := string(body)
		return Body{Text: s, Links: FindLinks(s)}
	}
}

func parseMIME(raw []byte) Body {
	ent, err := message.Read(bytes.NewReader(raw))
	if err != nil && ent == nil {
		s := string(raw)
		return Body{Text: s, Links: FindLinks(s)}
	}
	var plain, htmlText string
	var out Body
	walk(ent, func(e *message.Entity) {
		mt, params, _ := e.Header.ContentType()
		disp, dparams, _ := e.Header.ContentDisposition()
		data, _ := io.ReadAll(e.Body)
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		switch {
		case e.Header.Get("Content-Id") != "" && !strings.HasPrefix(mt, "text/") && !strings.HasPrefix(mt, "multipart/"):
			// Inline image of an HTML body.
			cid := strings.Trim(e.Header.Get("Content-Id"), "<> ")
			out.Attachments = append(out.Attachments, Part{Filename: orName(name, cid), MIMEType: mt, ContentID: cid, Data: data})
		case mt == "text/calendar":
			// Meeting invitations are shown as an invitation card.
			out.Attachments = append(out.Attachments, Part{Filename: orName(name, "pozvanka.ics"), MIMEType: mt, Data: data})
		case disp == "attachment" || (name != "" && !strings.HasPrefix(mt, "text/")):
			out.Attachments = append(out.Attachments, Part{Filename: name, MIMEType: mt, Data: data})
		case mt == "text/plain" && plain == "":
			plain = string(data)
		case mt == "text/html" && htmlText == "":
			var links []string
			out.HTML = string(data)
			htmlText, links = HTMLToText(string(data))
			out.Links = append(out.Links, links...)
		}
	})
	if plain != "" {
		out.Text = plain
	} else {
		out.Text = htmlText
	}
	out.Links = dedup(append(out.Links, FindLinks(out.Text)...))
	return out
}

func orName(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func walk(e *message.Entity, fn func(*message.Entity)) {
	if mr := e.MultipartReader(); mr != nil {
		for {
			p, err := mr.NextPart()
			if err != nil {
				return
			}
			walk(p, fn)
		}
	}
	fn(e)
}

var linkRe = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)

func FindLinks(s string) []string { return linkRe.FindAllString(s, -1) }

// HTMLToText is a small HTML to text converter: it keeps block structure and
// shows link targets next to their text, which also makes phishing links
// ("paypal.com" pointing elsewhere) visible to the reader.
func HTMLToText(src string) (string, []string) {
	z := html.NewTokenizer(strings.NewReader(src))
	var sb strings.Builder
	var links []string
	var href string
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return collapse(sb.String()), links
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "head", "title":
				if tt == html.StartTagToken {
					skip++
				}
			case "br":
				sb.WriteString("\n")
			case "p", "div", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "table", "blockquote":
				sb.WriteString("\n")
			case "li":
				sb.WriteString("\n • ")
			case "td", "th":
				sb.WriteString("\t")
			case "a":
				href = ""
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "href" {
						href = string(v)
					}
				}
				if strings.HasPrefix(href, "http") {
					links = append(links, href)
				}
			case "img":
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "alt" && len(v) > 0 {
						sb.WriteString("[obrázek: " + string(v) + "]")
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				if skip > 0 {
					skip--
				}
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				sb.WriteString("\n")
			case "a":
				if strings.HasPrefix(href, "http") {
					sb.WriteString(" <" + href + ">")
				}
				href = ""
			}
		case html.TextToken:
			if skip == 0 {
				raw := string(z.Text())
				text := strings.Join(strings.Fields(raw), " ")
				if text == "" {
					if raw != "" {
						sb.WriteString(" ")
					}
					continue
				}
				if strings.TrimLeft(raw, " \t\r\n") != raw {
					sb.WriteString(" ")
				}
				sb.WriteString(text)
				if strings.TrimRight(raw, " \t\r\n") != raw {
					sb.WriteString(" ")
				}
			}
		}
	}
}

var (
	blankLines = regexp.MustCompile(`\n[ \t]*(\n[ \t]*)+`)
	spaces     = regexp.MustCompile(` {2,}`)
	lineEdges  = regexp.MustCompile(`(?m)^ +| +$`)
)

func collapse(s string) string {
	s = spaces.ReplaceAllString(s, " ")
	s = lineEdges.ReplaceAllString(s, "")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
