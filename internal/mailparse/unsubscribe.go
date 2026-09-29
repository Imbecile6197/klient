package mailparse

import (
	"mime"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
)

// ParseHeaderBlock parses the raw RFC 822 header block Proton stores with each
// message. Proton's pre-parsed header map is incomplete for some messages and
// is lost in the offline cache, so the raw block is the source of truth;
// fallback fills anything the raw block does not have.
func ParseHeaderBlock(raw string, fallback map[string][]string) map[string][]string {
	// A lenient parser: net/mail rejects the whole block on a single malformed
	// line, which real-world mail has often enough to lose headers.
	out := map[string][]string{}
	dec := mime.WordDecoder{}
	var name, value string
	flush := func() {
		if name == "" {
			return
		}
		v := strings.TrimSpace(value)
		if d, err := dec.DecodeHeader(v); err == nil {
			v = d
		}
		key := textproto.CanonicalMIMEHeaderKey(name)
		out[key] = append(out[key], v)
		name, value = "", ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' { // folded continuation
			if name != "" {
				value += " " + strings.TrimSpace(line)
			}
			continue
		}
		flush()
		if i := strings.IndexByte(line, ':'); i > 0 {
			name, value = strings.TrimSpace(line[:i]), line[i+1:]
		}
	}
	flush()
	for k, v := range fallback {
		if _, ok := headerKey(out, k); !ok {
			out[k] = v
		}
	}
	return out
}

func headerKey(h map[string][]string, name string) (string, bool) {
	for k := range h {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

// Unsubscribe describes how a mailing list can be left (RFC 2369 / 8058).
type Unsubscribe struct {
	HTTP     string // https:// link
	Mailto   string // mailto: URI
	OneClick bool   // RFC 8058: POST "List-Unsubscribe=One-Click" to HTTP
}

func (u Unsubscribe) Any() bool { return u.HTTP != "" || u.Mailto != "" }

var angle = regexp.MustCompile(`<([^>]+)>`)

func headerValue(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return strings.Join(v, ", ")
		}
	}
	return ""
}

// ParseUnsubscribe reads List-Unsubscribe and List-Unsubscribe-Post.
// Only https links are accepted (plain http would leak the token).
func ParseUnsubscribe(headers map[string][]string) Unsubscribe {
	var u Unsubscribe
	for _, m := range angle.FindAllStringSubmatch(headerValue(headers, "List-Unsubscribe"), -1) {
		ref := strings.TrimSpace(m[1])
		p, err := url.Parse(ref)
		if err != nil {
			continue
		}
		switch strings.ToLower(p.Scheme) {
		case "https":
			if u.HTTP == "" {
				u.HTTP = ref
			}
		case "mailto":
			if u.Mailto == "" {
				u.Mailto = ref
			}
		}
	}
	post := strings.ToLower(headerValue(headers, "List-Unsubscribe-Post"))
	u.OneClick = u.HTTP != "" && strings.Contains(post, "list-unsubscribe=one-click")
	return u
}
