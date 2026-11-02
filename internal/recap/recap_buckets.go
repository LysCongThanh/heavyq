package recap

import (
	"sort"

	"github.com/LysCongThanh/heavyq/internal/flowview"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

func (r *Recap) addProven(it Item, merged func(string) bool) {
	t := it.At
	l := Line{ID: it.ID, Lane: it.Lane, At: &t, Commit: it.Commit, Text: it.ID + "  " + clean(it.Text, 50)}
	if it.Done {
		l.Action = "git show --stat " + it.Commit
	} else {
		l.Action, l.tick = "heavyq item done "+it.ID, true
	}
	if merged != nil && it.Commit != "" && merged(it.Commit) {
		r.Merged = append(r.Merged, l)
		return
	}
	r.ProvenNotMerged = append(r.ProvenNotMerged, l)
}

func claimedUnproven(it Item) Line {
	why := "ticked, nothing proves it"
	if it.State == itemstatus.Stale {
		why = "proven on older code, the code changed since"
	} else if it.Reason != "" {
		why = "ticked, " + clean(it.Reason, 50)
	}
	return Line{ID: it.ID, Lane: it.Lane, Text: it.ID + "  " + why, Action: "heavyq item done " + it.ID + " --run"}
}

func inFlight(it Item) Line {
	l := Line{ID: it.ID, Lane: it.Lane, Text: it.ID + "  " + or(it.Lane, "a lane") + " holds it", Action: "heavyq item done " + it.ID + " --run"}
	if !it.Expires.IsZero() {
		t := it.Expires
		l.At = &t
		l.Text += " until " + t.Local().Format("15:04")
	}
	return l
}

// addNeedsYou lists what the flow state says waits for a person and no blocked
// item already covers (a permission prompt has no item to claim).
func (r *Recap) addNeedsYou(st *flowview.State) {
	if st == nil {
		return
	}
	covered := map[string]bool{}
	for _, b := range r.Blocked {
		covered[b.ID] = true
	}
	for _, n := range st.NeedsYou {
		if n.Item != "" && covered[n.Item] {
			continue
		}
		l := Line{ID: n.Item, Lane: n.Lane, Text: or(n.Item, n.Lane) + "  " + clean(n.Reason, 60), Action: "heavyq top"}
		if !n.Since.IsZero() {
			t := n.Since
			l.At = &t
		}
		r.Blocked = append(r.Blocked, l)
	}
}

func or(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// sortLines orders a bucket for reading: by time, then id.
func sortLines(l []Line) {
	sort.SliceStable(l, func(i, j int) bool {
		if a, b := l[i].At, l[j].At; a != nil && b != nil && !a.Equal(*b) {
			return a.Before(*b)
		}
		return l[i].ID < l[j].ID
	})
}
