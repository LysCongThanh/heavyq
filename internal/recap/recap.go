// Package recap answers "how is it going, what landed, what should I do?" in
// one screen, from facts the caller has already gathered (items, the flow
// state, recent ledger records). Build is pure; Render turns the result into
// text, and the same value is the --json document. Every line ends with the
// command that acts on it.
package recap

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

// Schema is the version of the --json document.
const Schema = 1

// Item is one work item as the recap needs it (a flattened itemstatus.Status).
type Item struct {
	ID, State, Lane, Session, Commit, Needs, Reason, Text string
	At, Expires                                           time.Time
	Done                                                  bool
}

// Facts is everything Build looks at.
type Facts struct {
	Now, Since time.Time
	Goal       string
	Items      []Item
	// Records are the ledger records of this repository; Build keeps those in the window.
	Records []history.Record
	Session string         // when set, only this session's items and runs
	State   *flow.Snapshot // the materialised flow state; nil when there is none
	// Merged says whether a proven commit is reachable from MergeRef (the
	// default branch). nil when that cannot be told: every proven item is then
	// just "proven". A squash merge is not reachable, so false means "not on
	// MergeRef", which is all that is claimed.
	Merged   func(commit string) bool
	MergeRef string // "main": what Merged looks at, for the titles
}

// Line is one entry of a bucket.
type Line struct {
	ID      string     `json:"id,omitempty"`   // the item, when the line is about one
	Text    string     `json:"text"`           // what happened, one line
	Lane    string     `json:"lane,omitempty"` // who
	At      *time.Time `json:"at,omitempty"`   // when it landed, until when it is claimed, or since when it waits
	Count   int        `json:"count,omitempty"`
	Commit  string     `json:"commit,omitempty"`
	LostSec float64    `json:"lost_s,omitempty"` // wasted: run time spent on the repeats
	Action  string     `json:"action"`           // the command that acts on it
	tick    bool       // a proven item whose box is not ticked yet
}

// Progress counts the items that are proven out of all of them.
type Progress struct {
	Proven int `json:"proven"`
	Total  int `json:"total"`
}

// Forecast is the forecast part of the header.
type Forecast struct {
	Status     string     `json:"status"` // on_track, at_risk, unknown
	Confidence string     `json:"confidence"`
	Done       bool       `json:"done,omitempty"`
	ETA        *time.Time `json:"eta,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Samples    int        `json:"samples"`
}

// Best is the one action that would help most.
type Best struct {
	Command string `json:"command"`
	Why     string `json:"why"`
}

// LaneRow is a lane as shown by --verbose.
type LaneRow struct {
	Lane  string     `json:"lane"`
	State string     `json:"state"`
	Item  string     `json:"item,omitempty"`
	Since *time.Time `json:"since,omitempty"`
}

// Recap is the whole document (--json).
type Recap struct {
	Schema          int       `json:"schema"`
	Generated       time.Time `json:"generated"`
	Since           time.Time `json:"since"`
	Goal            string    `json:"goal,omitempty"`
	MergeRef        string    `json:"merge_ref,omitempty"` // set when merged-proven could be told apart
	Progress        Progress  `json:"progress"`
	Forecast        Forecast  `json:"forecast"`
	Constraint      string    `json:"constraint,omitempty"`
	NeedsYou        int       `json:"needs_you"`
	Best            *Best     `json:"best,omitempty"`
	Merged          []Line    `json:"merged"`
	ProvenNotMerged []Line    `json:"proven_not_merged"`
	ClaimedUnproven []Line    `json:"claimed_unproven"`
	DoneStale       []Line    `json:"done_stale"` // ticked, proven before, the code changed since (additive)
	Blocked         []Line    `json:"blocked"`
	Wasted          []Line    `json:"wasted"`
	InFlight        []Line    `json:"in_flight"`
	Lanes           []LaneRow `json:"lanes,omitempty"`
}

// Build derives the recap.
func Build(f Facts) Recap {
	r := Recap{Schema: Schema, Generated: f.Now, Since: f.Since, Goal: f.Goal,
		Merged: []Line{}, ProvenNotMerged: []Line{}, ClaimedUnproven: []Line{}, DoneStale: []Line{}, Blocked: []Line{}, Wasted: []Line{}, InFlight: []Line{}}
	if f.Merged != nil {
		r.MergeRef = f.MergeRef
	}
	items := f.Items
	if f.Session != "" {
		items = nil
		for _, it := range f.Items {
			if it.Session == f.Session {
				items = append(items, it)
			}
		}
	}
	running := 0
	var lastProof time.Time
	var firstOpen string
	for _, it := range items {
		r.Progress.Total++
		switch it.State {
		case itemstatus.Proven:
			r.Progress.Proven++
			if it.At.After(lastProof) {
				lastProof = it.At
			}
			if !it.At.Before(f.Since) {
				r.addProven(it, f.Merged)
			}
		case itemstatus.ClaimedDoneUnproven:
			r.ClaimedUnproven = append(r.ClaimedUnproven, claimedUnproven(it))
		case itemstatus.Stale:
			if it.Done {
				r.DoneStale = append(r.DoneStale, doneStale(it))
			} else if firstOpen == "" { // proved once, never ticked: open work, claimable like any
				firstOpen = it.ID
			}
		case itemstatus.Blocked:
			r.Blocked = append(r.Blocked, Line{ID: it.ID, Lane: it.Lane, Text: it.ID + "  needs: " + or(clean(it.Needs, 60), "an answer"),
				Action: "heavyq item claim " + it.ID})
		case itemstatus.Claimed:
			running++
			r.InFlight = append(r.InFlight, inFlight(it))
		case itemstatus.Open:
			if firstOpen == "" {
				firstOpen = it.ID
			}
		}
	}
	r.addNeedsYou(f.State)
	r.NeedsYou = len(r.Blocked)
	r.Wasted = wasted(f.Records, f.Since, f.Session)
	for _, l := range [][]Line{r.Merged, r.ProvenNotMerged, r.ClaimedUnproven, r.DoneStale, r.InFlight} {
		sortLines(l)
	}
	r.Forecast, r.Constraint = headerFacts(f, r.Progress.Total-r.Progress.Proven, running, lastProof)
	r.Best = best(r, firstOpen)
	if f.State != nil {
		for _, l := range f.State.Lanes {
			row := LaneRow{Lane: l.Lane, State: string(l.State), Item: l.Item}
			if !l.Since.IsZero() {
				t := l.Since
				row.Since = &t
			}
			r.Lanes = append(r.Lanes, row)
		}
	}
	return r
}
