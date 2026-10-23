//go:build darwin

package i18n

import (
	"os/exec"
	"strings"
)

// platformLang reads the macOS UI language. GUI apps launched from Finder
// often have LANG=C, so gettext-style env vars are useless; AppleLanguages
// is the real preference (system-wide, unless Klient overrode it in its domain).
func platformLang() string {
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	// Typical output: ( "cs-CZ", "en-GB", … )
	for _, part := range strings.FieldsFunc(string(out), func(r rune) bool {
		switch r {
		case '"', '(', ')', ',', '\n', '\t', ' ', '\r':
			return true
		}
		return false
	}) {
		if part == "" {
			continue
		}
		return normalize(part)
	}
	return ""
}
