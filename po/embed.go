// Package po holds the translation catalogs (GNU gettext .po files),
// embedded into the binary. The source strings in the code are English.
package po

import "embed"

// Files contains one <language>.po file per translation.
//
//go:embed *.po
var Files embed.FS
