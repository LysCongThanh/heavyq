package snapshot

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// loops: the same command failed at least twice on the same tree in a lane
// within LoopWindow. The agent is retrying code that cannot pass.
func loops(s Snapshot, recs []history.Record, now time.Time) []Exception {
	type key struct{ lane, tree, cmd string }
	type tally struct {
		n  int
		at time.Time
	}
	counts := map[key]*tally{}
	var order []key
	for _, r := range recs {
		if r.Lane == "" || r.Tree == "" || r.Exit == 0 || r.Outcome() != history.OutcomeRan ||
			r.Cache == "hit" || r.Cache == "coalesced" || now.Sub(r.End) > LoopWindow || r.End.After(now) {
			continue
		}
		k := key{r.Lane, r.Tree, r.Cmd}
		t := counts[k]
		if t == nil {
			t = &tally{}
			counts[k] = t
			order = append(order, k)
		}
		t.n++
		if r.End.After(t.at) {
			t.at = r.End
		}
	}
	var out []Exception
	for _, k := range order {
		t := counts[k]
		if t.n < 2 {
			continue
		}
		out = append(out, Exception{
			ID: strings.Join([]string{KindLoop, k.lane, k.tree, k.cmd, fmt.Sprint(t.n)}, "|"), Kind: KindLoop, Severity: SeverityCritical,
			Lane:     k.lane,
			Text:     fmt.Sprintf("%s ran %s %d times on the same code and it failed every time.", k.lane, quote(k.cmd), t.n),
			Evidence: Evidence{At: t.at, Tree: k.tree, Cmd: k.cmd, Count: t.n},
			Action:   cancelAction(s, k.lane),
		})
	}
	return out
}

// cancelAction stops the lane's job: the running one, else the waiting one.
// nil when the lane has nothing to stop.
func cancelAction(s Snapshot, lane string) *Action {
	var id string
	for _, t := range s.Waiting {
		if t.Lane == lane && !t.CancelRequested {
			id = t.ID
		}
	}
	for _, t := range s.Running {
		if t.Lane == lane && !t.CancelRequested {
			id = t.ID
		}
	}
	if id == "" {
		return nil
	}
	return &Action{Kind: ActionCancel, Target: id, Command: "heavyq cancel " + shellQuote(id)}
}

// starved: the head of the queue waited more than two thirds of the bypass
// delay, or a lane gave up within GiveUpWindow. One exception per lane.
func starved(s Snapshot, recs []history.Record, now time.Time) []Exception {
	gaveUp, lastGaveUp := map[string]int{}, map[string]time.Time{}
	var lanes []string
	for _, r := range recs {
		if r.Outcome() != history.OutcomeGaveUp || now.Sub(r.End) > GiveUpWindow || r.End.After(now) {
			continue
		}
		if gaveUp[r.Lane] == 0 {
			lanes = append(lanes, r.Lane)
		}
		gaveUp[r.Lane]++
		lastGaveUp[r.Lane] = r.End
	}
	head := longWaitingHead(s)
	if head != nil && gaveUp[head.Lane] == 0 {
		lanes = append(lanes, head.Lane)
	}
	var out []Exception
	for _, lane := range lanes {
		n := gaveUp[lane]
		e := Exception{Kind: KindStarved, Severity: SeverityWarning, Lane: lane,
			ID: fmt.Sprintf("%s|%s|%d", KindStarved, lane, n), Evidence: Evidence{At: lastGaveUp[lane], Count: n}}
		var clauses []string
		if head != nil && head.Lane == lane {
			clauses = append(clauses, fmt.Sprintf("has waited %s at the head of the queue (the bypass applies at %s)",
				dur(head.WaitS), dur(s.Config.ForceAfterS)))
			e.Evidence.At, e.Evidence.Cmd = head.Since, head.Cmd
			if head.Prio > 1 {
				e.Action = &Action{Kind: ActionRaise, Target: head.ID, Value: head.Prio - 1,
					Command: fmt.Sprintf("heavyq reprio %s %d", shellQuote(head.ID), head.Prio-1)}
			}
		}
		if n > 0 {
			clauses = append(clauses, "gave up "+times(n)+" in the last hour")
		}
		e.Text = lane + " " + strings.Join(clauses, " and ") + "."
		out = append(out, e)
	}
	return out
}

// longWaitingHead is the head of the queue when it is blocked everywhere and
// has waited more than two thirds of force_after; else nil.
func longWaitingHead(s Snapshot) *Waiting {
	if s.Config.ForceAfterS <= 0 {
		return nil
	}
	for i := range s.Waiting {
		w := &s.Waiting[i]
		if w.Position == 1 && w.WaitS > s.Config.ForceAfterS*2/3 && allBlocked(w.Reasons) {
			return w
		}
	}
	return nil
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
