package mailparse

import (
	"mime"
	"net/mail"
	"strings"
)

var wordDecoder = mime.WordDecoder{}

// DisplayAddress formats an address for people: `Libor Macák <x@y.cz>`.
// Unlike mail.Address.String it never RFC 2047-encodes the name, and the
// result still parses back with mail.ParseAddressList.
func DisplayAddress(a *mail.Address) string {
	if a == nil {
		return ""
	}
	name := strings.TrimSpace(a.Name)
	if dec, err := wordDecoder.DecodeHeader(name); err == nil {
		name = dec
	}
	if name == "" || strings.EqualFold(name, a.Address) {
		return a.Address
	}
	if strings.ContainsAny(name, `,;"<>@()[]:\`) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + a.Address + ">"
}

// DisplayName returns just the decoded name, or the address if there is none.
func DisplayName(a *mail.Address) string {
	if a == nil {
		return ""
	}
	if dec, err := wordDecoder.DecodeHeader(strings.TrimSpace(a.Name)); err == nil && dec != "" {
		return dec
	}
	return a.Address
}
