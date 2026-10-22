// Package status serves mbridge's own status page (SP14 §3.1). It is
// served from mbridge's own address, next to the API it reads, so it is
// not a magpie plugin and not bound by that sandbox.
package status

import _ "embed"

//go:embed page.html
var page []byte

// Page is the status page's HTML: one file, no external assets.
func Page() []byte { return page }
