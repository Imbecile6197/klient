package mailparse

import (
	"regexp"
	"strings"
)

// The Content-Security-Policy is the main line of defence for rendering mail
// HTML: no scripts, no frames, no forms, no network access except (on request)
// images and styles. It is inserted before anything else in the document so
// it applies before the parser meets any resource.
const (
	cspBlocked = `default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; form-action 'none'; frame-src 'none'; base-uri 'none'`
	cspRemote  = `default-src 'none'; img-src data: https: http:; style-src 'unsafe-inline' data: https:; font-src data: https:; form-action 'none'; frame-src 'none'; base-uri 'none'`
)

const baseStyle = `<style>html,body{margin:0;padding:0}body{padding:4px 2px;overflow-wrap:anywhere;font-family:sans-serif}img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}</style>`

var (
	remoteRef = regexp.MustCompile(`(?i)(?:src|background|poster|srcset)\s*=\s*["']?\s*(?:https?:)?//|url\(\s*["']?\s*(?:https?:)?//|@import`)
	cidRef    = regexp.MustCompile(`(?i)cid:([^"'\s)>]+)`)
)

// HasRemoteContent reports whether the HTML would load anything from the
// network (images, tracking pixels, web fonts, stylesheets).
func HasRemoteContent(html string) bool { return remoteRef.MatchString(html) }

// PrepareHTML returns a document safe to hand to the web view: inline images
// (cid:) replaced with data: URIs from inline, and a CSP that blocks remote
// content unless allowRemote.
func PrepareHTML(html string, inline map[string]string, allowRemote bool) string {
	html = cidRef.ReplaceAllStringFunc(html, func(m string) string {
		id := cidRef.FindStringSubmatch(m)[1]
		if uri, ok := inline[id]; ok {
			return uri
		}
		return m
	})
	csp := cspBlocked
	if allowRemote {
		csp = cspRemote
	}
	return `<meta http-equiv="Content-Security-Policy" content="` + csp + `">` +
		`<meta name="referrer" content="no-referrer">` + baseStyle + html
}

// PlainToHTML renders a plain-text body as HTML (for printing and for
// replies in the rich editor), escaping it and keeping line breaks.
func PlainToHTML(text string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return `<pre style="font-family:sans-serif">` + r.Replace(text) + `</pre>`
}
