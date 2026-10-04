package pgpmime

import (
	"bytes"
	"mime"
	"strings"
)

// HiddenSubject replaces the subject outside an encrypted message whose real
// subject travels inside (protected headers, as Thunderbird does).
const HiddenSubject = "..."

// ProtectHeaders copies headers (Subject, From, To…; already encoded as for
// the message header) into the top of an entity and marks it with
// protected-headers="v1", so the recipient's app shows them from inside
// the encryption.
func ProtectHeaders(entity []byte, headers [][2]string) []byte {
	head, body := split(entity)
	nl := "\r\n"
	if !strings.Contains(head, "\r\n") && strings.Contains(head, "\n") {
		nl = "\n"
	}
	mt, params := contentType(head)
	if mt == "" {
		return entity
	}
	params["protected-headers"] = "v1"
	var lines []string
	in := false
	for _, line := range strings.Split(head, nl) {
		if in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue // folded rest of the old Content-Type
		}
		in = false
		if k, _, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Type") {
			in = true
			lines = append(lines, "Content-Type: "+mime.FormatMediaType(mt, params))
			continue
		}
		lines = append(lines, line)
	}
	for _, h := range headers {
		if h[1] != "" {
			lines = append(lines, h[0]+": "+h[1])
		}
	}
	var out bytes.Buffer
	out.WriteString(strings.Join(lines, nl))
	out.WriteString(nl + nl)
	out.Write(body)
	return out.Bytes()
}

// ProtectedSubject returns the subject protected inside a decrypted entity,
// or "".
func ProtectedSubject(entity []byte) string {
	head, _ := split(entity)
	if _, params := contentType(head); params["protected-headers"] == "" {
		return ""
	}
	var val []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(head, "\r\n", "\n"), "\n") {
		if in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			val = append(val, strings.TrimSpace(line))
			continue
		}
		in = false
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Subject") {
			in = true
			val = append(val, strings.TrimSpace(v))
		}
	}
	if len(val) == 0 {
		return ""
	}
	raw := strings.Join(val, " ")
	if dec, err := new(mime.WordDecoder).DecodeHeader(raw); err == nil {
		return dec
	}
	return raw
}
