package recap

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/redact"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// minRepeats is how many identical failures make waste. One failure is
// information; the same one again, on the same code, is the agent trying the
// same thing twice.
const minRepeats = 2

// maxWasted keeps the bucket to what a person reads.
const maxWasted = 5

// wasted groups the failing runs in the window by their signature: the run
// id (tree, exact argv, subdir, env, base: the one definition of "the same
// run"), else the command hash, else the command text. A failure that started
// over a resource limit (under_load) is the machine's, not the code's, and a
// replay from the cache is not a run, so neither counts.
func wasted(recs []history.Record, since time.Time, session string) []Line {
	type group struct {
		n     int
		lost  float64
		cmd   string
		lanes []string
	}
	groups := map[string]*group{}
	var order []string
	for _, r := range recs {
		if r.End.Before(since) || r.Exit == 0 || r.Outcome() != history.OutcomeRan || r.Loaded() ||
			r.Cache == "hit" || r.Cache == "coalesced" || (session != "" && r.Session != session) {
			continue
		}
		key := r.RunID
		if key == "" {
			key = r.CmdHash
		}
		if key == "" {
			key = "cmd:" + r.Cmd
		}
		g := groups[key]
		if g == nil {
			g = &group{cmd: r.Cmd}
			groups[key] = g
			order = append(order, key)
		}
		g.n++
		g.lost += r.RunSecs
		if !contains(g.lanes, r.Lane) {
			g.lanes = append(g.lanes, r.Lane)
		}
	}
	var out []Line
	for _, k := range order {
		g := groups[k]
		if g.n < minRepeats {
			continue
		}
		lane := g.lanes[len(g.lanes)-1]
		out = append(out, Line{Lane: lane, Count: g.n, LostSec: g.lost,
			Text:   fmt.Sprintf("%s  failed %dx, %s lost (%s)", clean(redact.Command(g.cmd), 48), g.n, human(g.lost), strings.Join(g.lanes, ", ")),
			Action: "heavyq why " + lane})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LostSec > out[j].LostSec
	})
	if len(out) > maxWasted {
		out = out[:maxWasted]
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// human is a short duration: 45s, 8m, 1h20m.
func human(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	}
	m := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

// clean makes untrusted text (an item's text, a reason) safe for one line:
// control characters dropped, cut to n cells.
func clean(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return width.Truncate(strings.TrimSpace(s), n, "...")
}
