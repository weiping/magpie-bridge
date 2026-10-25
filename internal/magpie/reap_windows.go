//go:build windows

package magpie

import "time"

// reapHosts is a no-op on Windows: magpie's plugin hosts there are not
// left orphaned the way a Unix parent's exit leaves them.
func reapHosts(time.Time) {}
