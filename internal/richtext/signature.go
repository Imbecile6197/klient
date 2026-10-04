package richtext

import (
	"regexp"
	"strings"
)

var (
	sigBody    = regexp.MustCompile(`(?is)<body[^>]*>(.*)</body>`)
	sigDropped = regexp.MustCompile(`(?is)<(script|head|title|iframe|object|embed|form)\b.*?</(script|head|title|iframe|object|embed|form)\s*>|<(meta|link|base|!doctype)\b[^>]*>|</?(html|body)\b[^>]*>`)
	sigEvents  = regexp.MustCompile(`(?i)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	sigJSLinks = regexp.MustCompile(`(?i)(href|src)\s*=\s*(["']?)\s*javascript:[^"'\s>]*`)
)

// CleanSignature prepares an HTML signature for the body of an e-mail: of a
// whole page only the body is kept, and scripts, event handlers, frames and
// forms are removed (recipients' mail apps would drop them anyway).
func CleanSignature(src string) string {
	if m := sigBody.FindStringSubmatch(src); m != nil {
		src = m[1]
	}
	src = sigDropped.ReplaceAllString(src, "")
	src = sigEvents.ReplaceAllString(src, "")
	src = sigJSLinks.ReplaceAllString(src, `$1=$2#`)
	return strings.TrimSpace(src)
}

// SignatureHTML wraps a cleaned signature for the message body.
func SignatureHTML(sig string) string {
	return `<div class="klient-signature" style="margin-top:4px">` + sig + `</div>`
}
