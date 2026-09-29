// Package docs holds the user documentation, embedded into the binary so
// the Help menu works in every installation.
package docs

import _ "embed"

// Index is the complete user documentation (self-contained HTML).
//
//go:embed index.html
var Index []byte
