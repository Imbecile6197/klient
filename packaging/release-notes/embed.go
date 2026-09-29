// Package releasenotes holds the release notes of each version (Markdown,
// an "## English" and an "## Česky" section). They are published with the
// GitHub release and shown in Klient as "What's New".
package releasenotes

import "embed"

// Files contains one <version>.md per release.
//
//go:embed *.md
var Files embed.FS
