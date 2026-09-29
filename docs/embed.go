// Package docs holds the user documentation, embedded into the binary so
// the Help menu works in every installation.
package docs

import "embed"

// Files contains index.html (English) and index.cs.html (Czech); the two
// pages link to each other.
//
//go:embed index.html index.cs.html
var Files embed.FS

// Page is the file name of the documentation in a language.
func Page(lang string) string {
	if lang == "cs" {
		return "index.cs.html"
	}
	return "index.html"
}
