// Package spell checks spelling with the system's Enchant library and its
// dictionaries (Hunspell). Enchant is loaded at run time, so Klient builds
// and runs without it – spell checking is then simply not available.
package spell

import (
	"strings"
	"unicode"
)

// Word is a word of a text; Start and End count characters (runes), as
// GtkTextBuffer offsets do.
type Word struct {
	Start, End int
	Text       string
}

// Words returns the words worth checking: not inside quoted lines (">"),
// links or e-mail addresses, and not numbers, single letters or
// abbreviations in capitals.
func Words(text string) []Word {
	var out []Word
	runes := []rune(text)
	lineStart := true
	quoted := false
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == '\n' {
			lineStart, quoted = true, false
			i++
			continue
		}
		if lineStart {
			if r == '>' {
				quoted = true
			}
			if !unicode.IsSpace(r) {
				lineStart = false
			}
		}
		if unicode.IsSpace(r) {
			i++
			continue
		}
		// A whitespace-separated token: links and addresses are skipped whole.
		j := i
		for j < len(runes) && !unicode.IsSpace(runes[j]) {
			j++
		}
		token := string(runes[i:j])
		if !quoted && !isLinkLike(token) {
			out = append(out, wordsIn(runes, i, j)...)
		}
		i = j
	}
	return out
}

func isLinkLike(t string) bool {
	l := strings.ToLower(t)
	return strings.Contains(l, "://") || strings.HasPrefix(l, "www.") || strings.Contains(l, "@") ||
		strings.Contains(l, "/") || strings.Contains(l, "\\") || strings.Count(l, ".") >= 2
}

// wordsIn splits runes[from:to] into words: letters, with an apostrophe or
// hyphen allowed inside (don't, česko-slovenský).
func wordsIn(runes []rune, from, to int) []Word {
	var out []Word
	i := from
	for i < to {
		if !unicode.IsLetter(runes[i]) {
			i++
			continue
		}
		j := i
		for j < to {
			r := runes[j]
			if unicode.IsLetter(r) || unicode.IsMark(r) {
				j++
				continue
			}
			if (r == '\'' || r == '’' || r == '-') && j+1 < to && unicode.IsLetter(runes[j+1]) {
				j++
				continue
			}
			break
		}
		// Letters glued to digits (A4, 2x) are codes, not words.
		glued := (i > from && unicode.IsDigit(runes[i-1])) || (j < to && unicode.IsDigit(runes[j]))
		w := string(runes[i:j])
		if !glued && worthChecking(w) {
			out = append(out, Word{Start: i, End: j, Text: w})
		}
		i = j
	}
	return out
}

func worthChecking(w string) bool {
	n := 0
	upper := true
	for _, r := range w {
		if unicode.IsLetter(r) {
			n++
			if !unicode.IsUpper(r) {
				upper = false
			}
		}
	}
	return n > 1 && !upper // single letters and ABBREVIATIONS are left alone
}
