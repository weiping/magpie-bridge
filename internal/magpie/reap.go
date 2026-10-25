//go:build !windows

package magpie

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// reapHosts kills the plugin hosts a CLI call left behind: a `magpie` run
// starts magpie's plugin host (a Bun process), and a host whose work never
// settles outlives the CLI that started it — `host.js` exits on a closed
// stdin only when nothing is pending. Each call then left one orphan, and
// the daemon's status page and its 30 s aggregator call the CLI often: on
// 2026-10-25 595 orphans held 7 GB, the swap filled, and the routing
// classifier timed out behind them.
//
// Only the orphans are taken: a host whose parent is 1 (its launcher is
// gone) and that started within the minutes before this call. One magpie
// itself is running (its parent is magpie) is not touched, and neither is
// one another client still holds. A host that has not been reparented yet
// when its call ends is caught by the next call's sweep.
func reapHosts(started time.Time) {
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(120 * time.Millisecond) // a launcher's exit reparents it
		}
		out, err := exec.Command("ps", "-eo", "pid=,ppid=,lstart=,command=").Output()
		if err != nil {
			return
		}
		// lstart is "Sun Oct 25 08:17:09 2026"; compare it as a time
		const layout = "Mon Jan _2 15:04:05 2006"
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || f[1] != "1" || !strings.Contains(line, "plugin-host/host-") {
				continue
			}
			at, err := time.ParseInLocation(layout, strings.Join(f[2:7], " "), time.Local)
			if err != nil || at.Before(started.Add(-5*time.Minute)) {
				continue
			}
			if pid, err := strconv.Atoi(f[0]); err == nil {
				_ = exec.Command("kill", strconv.Itoa(pid)).Run()
			}
		}
	}
}
