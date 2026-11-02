package flow

import (
	"encoding/json"
	"time"
)

// RunInfo is the "run" object of state.json: the open run span of
// internal/runspan (a preflighted, usually unattended stretch of work).
type RunInfo struct {
	ID          string     `json:"id"`
	Started     time.Time  `json:"started"`
	ExpectedEnd *time.Time `json:"expected_end"` // null when the run has no stated length
	Goal        string     `json:"goal"`
	Items       int        `json:"items"` // how many items the run planned
	Parallel    int        `json:"parallel"`
}

// runJSON is what Snapshot.Run holds for r: "null" without a run.
func runJSON(r *RunInfo) json.RawMessage {
	if r == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(r)
	if err != nil { // plain strings, numbers and times: cannot happen
		return json.RawMessage("null")
	}
	return b
}

// RunInfo decodes the run object. It is nil when there is none or when it does
// not parse: the field is kept raw so that one of another shape (a field of
// another type, written by another version) never makes the whole state
// unreadable.
func (s *Snapshot) RunInfo() *RunInfo {
	if len(s.Run) == 0 || string(s.Run) == "null" {
		return nil
	}
	var r RunInfo
	if json.Unmarshal(s.Run, &r) != nil || r.ID == "" && r.Started.IsZero() && r.ExpectedEnd == nil {
		return nil
	}
	return &r
}

// Empty reports a state with nothing to show: no items and nothing that needs a
// person.
func (s *Snapshot) Empty() bool { return s.Items.Total == 0 && len(s.NeedsYou) == 0 }

// NeedCount is how many things wait for a person: the listed needs, or the
// blocked items when the list is shorter.
func (s *Snapshot) NeedCount() int { return max(len(s.NeedsYou), s.Items.Blocked) }

// AllProven: there are items and every one is proven.
func (s *Snapshot) AllProven() bool { return s.Items.Total > 0 && s.Items.Proven >= s.Items.Total }
