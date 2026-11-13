// Package docs holds the user documentation, embedded into the binary so
// the Help menu works in every installation.
package docs

import "embed"

// Files contains index.html (English), index.cs.html (Czech) and
// index.de.html (German), which link to each other, and the main screenshot
// shown in them.
//
//go:embed index.html index.cs.html index.de.html screenshots/en/main.png screenshots/cs/main.png screenshots/de/main.png
var Files embed.FS

// Page is the file name of the documentation in a language.
func Page(lang string) string {
	switch lang {
	case "cs":
		return "index.cs.html"
	case "de":
		return "index.de.html"
	}
	return "index.html"
}
