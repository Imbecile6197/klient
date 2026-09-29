// Package docs holds the user documentation, embedded into the binary so
// the Help menu works in every installation.
package docs

import "embed"

// Files contains index.html (English) and index.cs.html (Czech), which
// link to each other, and the main screenshot shown in them.
//
//go:embed index.html index.cs.html screenshots/en/main.png screenshots/cs/main.png
var Files embed.FS

// Page is the file name of the documentation in a language.
func Page(lang string) string {
	if lang == "cs" {
		return "index.cs.html"
	}
	return "index.html"
}
