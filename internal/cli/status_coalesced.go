package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// printCoalesced shows runs that wait for an identical in-flight run
// (--cache) instead of queueing a duplicate. Nothing is printed when none do.
func printCoalesced(w io.Writer, now time.Time) {
	f := &cache.Flight{Dir: filepath.Join(QueueDir(), "inflight")}
	groups := f.Groups(procinfo.Default.Alive)
	if len(groups) == 0 {
		return
	}
	fmt.Fprintln(w, "COALESCED (identical runs reuse one result)")
	for _, g := range groups {
		lanes := make([]string, 0, len(g.Followers))
		for _, p := range g.Followers {
			lanes = append(lanes, p.Lane)
		}
		cmd := g.Followers[0].Cmd
		leader := "an identical run"
		if g.HasLeader {
			leader = fmt.Sprintf("lane %s's run (pid %d, %s)", g.Leader.Lane, g.Leader.Pid, fmtDur(now.Sub(g.Leader.Since)))
			cmd = g.Leader.Cmd
		}
		fmt.Fprintf(w, "  %d waiting on %s: %s\n", len(g.Followers), leader, cmd)
		fmt.Fprintf(w, "  %-3s waiting: %s\n", "", strings.Join(lanes, ", "))
	}
}
