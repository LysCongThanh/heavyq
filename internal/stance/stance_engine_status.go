package stance

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/lease"
)

// Status is what `heavyq stance status` shows about a lane.
type Status struct {
	Lane  string `json:"lane"`
	Muted bool   `json:"muted"`
	// Active is the stance the lane is in, nil when none.
	Active *Active `json:"active,omitempty"`
	// Minutes is how long the lane has been in it; LeftMinutes how much of the
	// time box remains (0 without one, or when it ran out).
	Minutes     int `json:"minutes,omitempty"`
	LeftMinutes int `json:"left_minutes,omitempty"`
	// Record is the decision record the exit waits for: its path, whether it
	// exists, and what its structure still lacks. Nil when the stance needs none.
	Record *RecordStatus `json:"record,omitempty"`
	// Evidence is what the triggers see right now.
	Evidence EvidenceView `json:"evidence"`
	// Untrusted lists repo stances that are listed but never injected.
	Untrusted []string `json:"untrusted,omitempty"`
}

// RecordStatus is the structure check of a lane's decision record.
type RecordStatus struct {
	Path     string   `json:"path"`
	Exists   bool     `json:"exists"`
	Problems []string `json:"problems,omitempty"`
}

// EvidenceView is Evidence without the prompt, for display.
type EvidenceView struct {
	FailureClass string `json:"failure_class,omitempty"`
	FailureTrees int    `json:"failure_trees"`
	ChurnReverts int    `json:"churn_reverts"`
	ChurnRewrite int    `json:"churn_rewrites"`
}

// Status reads the state of lane for display. It reads evidence (ledger index
// and git) even when the lane is in a stance, so the user sees what a trigger
// would see; it changes nothing but a time box that ran out.
func (e Env) Status(cwd, lane string) (Status, error) {
	out := Status{Lane: lane}
	set, err := e.LoadSet(cwd)
	if err != nil {
		return out, err
	}
	for _, s := range set.All {
		if set.Untrusted(s) {
			out.Untrusted = append(out.Untrusted, s.Name)
		}
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return out, err
	}
	now := e.now()
	if err := store.Update(lane, func(st *State) ([]Event, error) {
		if ex := st.Settle(now); ex != nil {
			return []Event{*ex}, nil
		}
		return nil, nil
	}); err != nil {
		return out, err
	}
	st, err := store.Load(lane)
	if err != nil {
		return out, err
	}
	out.Muted, out.Active = st.Muted, st.Active
	ev, _ := e.CollectEvidence(cwd, lane, set.All) // what could be read is shown
	out.Evidence = EvidenceView{ev.Failure.Label, ev.Failure.Trees, ev.Churn.Reverts, ev.Churn.Rewrites}
	if a := st.Active; a != nil {
		out.Minutes = int(now.Sub(a.Since) / time.Minute)
		if !a.Expires.IsZero() {
			out.LeftMinutes = max(0, int(a.Expires.Sub(now)/time.Minute))
		}
		if s, ok := Find(set.All, a.Stance); ok && s.Exit.DecisionRecord {
			out.Record = e.recordStatus(cwd, lane, a.Item)
		}
	}
	return out, nil
}

func (e Env) recordStatus(cwd, lane, item string) *RecordStatus {
	top, err := workTreeTop(cwd)
	if err != nil || (item != "" && lease.ValidateItem(item) != nil) {
		return nil
	}
	rs := &RecordStatus{Path: DecisionPath(top, DecisionKey(item, lane))}
	data, exists, err := ReadDecision(rs.Path)
	rs.Exists = exists
	switch {
	case err != nil:
		rs.Problems = []string{err.Error()}
	case exists:
		rs.Problems = DecisionProblems(string(data))
	}
	return rs
}
