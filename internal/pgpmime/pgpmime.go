// Package pgpmime signs, encrypts, decrypts and verifies OpenPGP mail in the
// PGP/MIME format (RFC 3156) used by Thunderbird, K-9/Thunderbird mobile,
// Mailvelope and others, and finds recipients' keys (WKD, keys.openpgp.org,
// Autocrypt).
package pgpmime

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/Imbecile6197/klient/internal/i18n"
)

func boundary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "klient-" + hex.EncodeToString(b)
}

// crlf normalises line endings: signatures are made over CRLF text.
func crlf(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}

// Sign wraps a MIME entity (headers + body) in multipart/signed. It returns
// the Content-Type of the result and its body.
func Sign(entity []byte, signer *crypto.KeyRing) (string, []byte, error) {
	entity = bytes.TrimRight(crlf(entity), "\r\n")
	sig, err := signer.SignDetached(crypto.NewPlainMessage(entity))
	if err != nil {
		return "", nil, err
	}
	armored, err := sig.GetArmored()
	if err != nil {
		return "", nil, err
	}
	b := boundary()
	var out bytes.Buffer
	out.WriteString("This is an OpenPGP/MIME signed message (RFC 4880 and 3156)\r\n")
	out.WriteString("--" + b + "\r\n")
	out.Write(entity)
	out.WriteString("\r\n--" + b + "\r\n")
	out.WriteString("Content-Type: application/pgp-signature; name=\"signature.asc\"\r\n")
	out.WriteString("Content-Description: OpenPGP digital signature\r\n")
	out.WriteString("Content-Disposition: attachment; filename=\"signature.asc\"\r\n\r\n")
	out.Write(crlf([]byte(armored)))
	out.WriteString("\r\n--" + b + "--\r\n")
	ct := fmt.Sprintf(`multipart/signed; micalg=pgp-sha256; protocol="application/pgp-signature"; boundary="%s"`, b)
	return ct, out.Bytes(), nil
}

// Encrypt encrypts (and signs, if signer is set) a MIME entity for the
// recipients' keys as multipart/encrypted.
func Encrypt(entity []byte, recipients, signer *crypto.KeyRing) (string, []byte, error) {
	msg, err := recipients.Encrypt(crypto.NewPlainMessage(crlf(entity)), signer)
	if err != nil {
		return "", nil, err
	}
	armored, err := msg.GetArmored()
	if err != nil {
		return "", nil, err
	}
	b := boundary()
	var out bytes.Buffer
	out.WriteString("This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156)\r\n")
	out.WriteString("--" + b + "\r\n")
	out.WriteString("Content-Type: application/pgp-encrypted\r\nContent-Description: PGP/MIME version identification\r\n\r\nVersion: 1\r\n\r\n")
	out.WriteString("--" + b + "\r\n")
	out.WriteString("Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n")
	out.WriteString("Content-Description: OpenPGP encrypted message\r\n")
	out.WriteString("Content-Disposition: inline; filename=\"encrypted.asc\"\r\n\r\n")
	out.Write(crlf([]byte(armored)))
	out.WriteString("\r\n--" + b + "--\r\n")
	ct := fmt.Sprintf(`multipart/encrypted; protocol="application/pgp-encrypted"; boundary="%s"`, b)
	return ct, out.Bytes(), nil
}

// ---- Reading ---------------------------------------------------------------------

// Kind of protection found on a message.
type Kind int

const (
	None Kind = iota
	Signed
	Encrypted
)

// split returns the header block and body of a message or entity.
func split(raw []byte) (string, []byte) {
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if i := bytes.Index(raw, []byte(sep)); i >= 0 {
			return string(raw[:i]), raw[i+len(sep):]
		}
	}
	return string(raw), nil
}

// contentType reads the (unfolded) Content-Type of a header block.
func contentType(header string) (string, map[string]string) {
	var val []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(header, "\r\n", "\n"), "\n") {
		if in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			val = append(val, strings.TrimSpace(line))
			continue
		}
		in = false
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Type") {
			in = true
			val = append(val, strings.TrimSpace(v))
		}
	}
	mt, params, err := mime.ParseMediaType(strings.Join(val, " "))
	if err != nil {
		return "", nil
	}
	return strings.ToLower(mt), params
}

// Detect tells whether a raw message is PGP/MIME signed or encrypted.
func Detect(raw []byte) Kind {
	h, _ := split(raw)
	mt, params := contentType(h)
	switch {
	case mt == "multipart/encrypted" && strings.EqualFold(params["protocol"], "application/pgp-encrypted"):
		return Encrypted
	case mt == "multipart/signed" && strings.EqualFold(params["protocol"], "application/pgp-signature"):
		return Signed
	}
	return None
}

// parts splits a multipart body into its raw parts (exact bytes).
func parts(body []byte, b string) [][]byte {
	body = crlf(body)
	delim := []byte("--" + b)
	var out [][]byte
	chunks := bytes.Split(body, delim)
	for i, c := range chunks {
		if i == 0 {
			continue // preamble
		}
		if bytes.HasPrefix(c, []byte("--")) {
			break // closing delimiter
		}
		c = bytes.TrimPrefix(c, []byte("\r\n"))
		c = bytes.TrimSuffix(c, []byte("\r\n")) // the CRLF before the next delimiter
		out = append(out, c)
	}
	return out
}

// ErrNoKey means the message is encrypted but no private key can open it.
var ErrNoKey = errors.New(i18n.T("the message is encrypted, but you have no key that can open it"))

// Decrypt opens a multipart/encrypted message and returns the inner MIME
// entity. verify is the sender's key (may be nil); verr is the result of
// the signature check (nil = valid, only meaningful with verify).
func Decrypt(raw []byte, own, verify *crypto.KeyRing) (entity []byte, verr error, err error) {
	h, body := split(raw)
	_, params := contentType(h)
	ps := parts(body, params["boundary"])
	if len(ps) < 2 {
		return nil, nil, errors.New(i18n.T("damaged encrypted message"))
	}
	_, data := split(ps[1])
	msg, err := crypto.NewPGPMessageFromArmored(string(data))
	if err != nil {
		return nil, nil, fmt.Errorf(i18n.T("damaged encrypted message: %w"), err)
	}
	if own == nil {
		return nil, nil, ErrNoKey
	}
	plain, err := own.Decrypt(msg, nil, 0)
	if err != nil {
		return nil, nil, ErrNoKey
	}
	entity = plain.GetBinary()
	if verify != nil {
		_, verr = own.Decrypt(msg, verify, crypto.GetUnixTime())
	} else {
		verr = errors.New("no key")
	}
	// The inner entity may itself be multipart/signed (sign-then-encrypt).
	if Detect(entity) == Signed {
		inner, v := Verify(entity, verify)
		return inner, v, nil
	}
	return entity, verr, nil
}

// Verify checks a multipart/signed message; it returns the signed entity
// and nil if the signature is valid.
func Verify(raw []byte, verify *crypto.KeyRing) ([]byte, error) {
	h, body := split(raw)
	_, params := contentType(h)
	ps := parts(body, params["boundary"])
	if len(ps) < 2 {
		return raw, errors.New(i18n.T("damaged signature"))
	}
	if verify == nil {
		return ps[0], errors.New("no key")
	}
	_, sigData := split(ps[1])
	sig, err := crypto.NewPGPSignatureFromArmored(string(sigData))
	if err != nil {
		return ps[0], err
	}
	return ps[0], verify.VerifyDetached(crypto.NewPlainMessage(ps[0]), sig, crypto.GetUnixTime())
}

// ---- Autocrypt -------------------------------------------------------------------

// AutocryptHeader is the value of the Autocrypt header advertising a key.
func AutocryptHeader(email string, key *crypto.Key) (string, error) {
	pub, err := key.GetPublicKey()
	if err != nil {
		return "", err
	}
	data := base64.StdEncoding.EncodeToString(pub)
	var sb strings.Builder
	sb.WriteString("addr=" + email + "; keydata=")
	// Fold into lines the mail writer can wrap at the spaces.
	for i := 0; i < len(data); i += 72 {
		sb.WriteString(" " + data[i:min(len(data), i+72)])
	}
	return sb.String(), nil
}

// ParseAutocrypt reads an Autocrypt header and returns the armored key if
// it belongs to from.
func ParseAutocrypt(value, from string) (string, bool) {
	var addr, keydata string
	for _, attr := range strings.Split(value, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(attr), "=")
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "addr":
			addr = strings.TrimSpace(v)
		case "keydata":
			keydata = strings.Join(strings.Fields(v), "")
		}
	}
	if !strings.EqualFold(addr, from) || keydata == "" {
		return "", false
	}
	bin, err := base64.StdEncoding.DecodeString(keydata)
	if err != nil {
		return "", false
	}
	key, err := crypto.NewKey(bin)
	if err != nil {
		return "", false
	}
	armored, err := key.GetArmoredPublicKey()
	return armored, err == nil
}

// ---- WKD ---------------------------------------------------------------------------

const zbase32 = "ybndrfg8ejkmcpqxot1uwisza345h769"

func zbase32Encode(b []byte) string {
	var sb strings.Builder
	var buf, bits uint
	for _, c := range b {
		buf = buf<<8 | uint(c)
		bits += 8
		for bits >= 5 {
			sb.WriteByte(zbase32[(buf>>(bits-5))&31])
			bits -= 5
		}
	}
	if bits > 0 {
		sb.WriteByte(zbase32[(buf<<(5-bits))&31])
	}
	return sb.String()
}

// WKDURLs are the Web Key Directory addresses of an e-mail address
// (advanced method first, then direct).
func WKDURLs(email string) []string {
	local, domain, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok {
		return nil
	}
	domain = strings.ToLower(domain)
	sum := sha1.Sum([]byte(strings.ToLower(local)))
	h := zbase32Encode(sum[:])
	l := "?l=" + urlQueryEscape(local)
	return []string{
		"https://openpgpkey." + domain + "/.well-known/openpgpkey/" + domain + "/hu/" + h + l,
		"https://" + domain + "/.well-known/openpgpkey/hu/" + h + l,
	}
}

func urlQueryEscape(s string) string {
	var sb strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0 {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}
