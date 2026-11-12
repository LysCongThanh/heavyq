package flow

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
)

// SchemaVersion is the "schema" of state.json. Other agents read the file in
// parallel: a field may be added, never renamed or retyped, without telling
// the lead.
const SchemaVersion = 1

// maxEvents bounds the recent events a snapshot carries.
const maxEvents = 10

// Snapshot is the materialised flow state: the one document the status line,
// top, recap and the brief read (<git common dir>/heavyq/flow/state.json).
type Snapshot struct {
	Schema  int       `json:"schema"`
	Repo    string    `json:"repo"`
	Updated time.Time `json:"updated"`
	Goal    string    `json:"goal"`
	// Run is the open run span (RunInfo), null when no run is declared. Kept raw
	// so that a shape of another version never makes the state unreadable.
	Run        json.RawMessage `json:"run"`
	Items      ItemCounts      `json:"items"`
	NeedsYou   []NeedsYou      `json:"needs_you"`
	Constraint Constraint      `json:"constraint"`
	Throughput Throughput      `json:"throughput"`
	Lanes      []LaneState     `json:"lanes"`
	Session    *SessionInfo    `json:"session"`
	// Added after the first contract, additive: readers that do not know them
	// ignore them.
	Fleet  Fleet       `json:"fleet"`
	Events []EventNote `json:"events"`
	// Baseline is what the constraint finder has learned is normal, as of this
	// computation: the next refresh reads it back (Input.Baseline).
	Baseline *Baseline `json:"baseline,omitempty"`
	// Quota is the open usage-limit episode, one entry for the whole fleet; null
	// when there is none (additive).
	Quota *QuotaInfo `json:"quota"`
}

// NeedsYou is one thing only a person can supply.
type NeedsYou struct {
	Item    string    `json:"item"`
	Lane    string    `json:"lane"`
	Session string    `json:"session"`
	Reason  string    `json:"reason"`
	Since   time.Time `json:"since"`
}

// LaneState is what one active lane is doing right now.
type LaneState struct {
	Lane  string    `json:"lane"`
	State State     `json:"state"`
	Item  string    `json:"item"`
	Since time.Time `json:"since"`
}

// EventNote is a recent hook observation worth showing: a turn that ended on
// an API error, or a model change point.
type EventNote struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Lane    string    `json:"lane"`
	Session string    `json:"session"`
	Detail  string    `json:"detail"`
}

// Build is the whole derivation: timelines, aggregates and the snapshot.
func Build(in Input) Snapshot {
	tls, holds := derive(in)
	fleet, constraint, tp := aggregate(in, tls, holds)
	blocks := in.blocks()
	quota := deriveQuota(in)
	snap := Snapshot{
		Schema: SchemaVersion, Repo: in.Repo, Updated: in.Now.UTC(), Goal: in.Goal, Run: runJSON(in.Run),
		Items: countItems(in.Items), NeedsYou: needsYou(in, blocks), Constraint: constraint, Throughput: tp,
		Lanes: lanesNow(in, tls, blocks), Session: in.Session, Fleet: fleet, Events: recentEvents(in),
		Baseline: NextBaseline(in.Baseline, fleet, in.Now), Quota: quota,
	}
	if quota != nil { // one entry for the fleet, not one per stopped lane
		snap.NeedsYou = append(snap.NeedsYou, NeedsYou{Lane: "fleet", Reason: QuotaText(quota, in.Now.Location()), Since: quota.Since.UTC()})
		sort.SliceStable(snap.NeedsYou, func(i, j int) bool { return snap.NeedsYou[i].Since.Before(snap.NeedsYou[j].Since) })
	}
	return snap
}

// needsYou lists the open blocks of lanes and the items blocked on a need,
// oldest first. A hook block names the item its lane holds.
func needsYou(in Input, blocks []Block) []NeedsYou {
	heldBy := map[string]string{} // lane -> item with a live lease
	for _, f := range in.Items {
		if f.Status.State == itemstatus.Claimed && f.Status.Lane != "" {
			if cur, ok := heldBy[f.Status.Lane]; !ok || f.Status.ID < cur {
				heldBy[f.Status.Lane] = f.Status.ID
			}
		}
	}
	out := []NeedsYou{}
	for _, b := range blocks {
		if b.Open() {
			out = append(out, NeedsYou{Item: heldBy[b.Lane], Lane: b.Lane, Session: b.Session, Reason: b.Reason, Since: b.Since.UTC()})
		}
	}
	for _, f := range in.Items {
		if st := f.Status; st.State == itemstatus.Blocked {
			out = append(out, NeedsYou{Item: st.ID, Lane: st.Lane, Session: st.Session,
				Reason: fmt.Sprintf("blocked: needs %s", st.Needs), Since: st.At.UTC()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// lanesNow is the current state of every lane that is active at now: an open
// block wins; else the state of its most recently active timeline.
func lanesNow(in Input, tls []Timeline, blocks []Block) []LaneState {
	cur := map[string]LaneState{}
	last := map[string]time.Time{}
	for _, t := range tls {
		st, since, ok := t.Current(in.Now, ActiveGap)
		if !ok || t.Lane == "" || in.GoneLanes[t.Lane] {
			continue
		}
		end := t.Spans[len(t.Spans)-1].End
		if prev, seen := last[t.Lane]; seen && !end.After(prev) {
			continue
		}
		last[t.Lane] = end
		item := ""
		if t.Kind == KindItem {
			item = t.Key
		}
		cur[t.Lane] = LaneState{Lane: t.Lane, State: st, Item: item, Since: since.UTC()}
	}
	for _, b := range blocks {
		if b.Open() {
			l := cur[b.Lane]
			cur[b.Lane] = LaneState{Lane: b.Lane, State: BlockedHuman, Item: l.Item, Since: b.Since.UTC()}
		}
	}
	out := make([]LaneState, 0, len(cur))
	for _, l := range cur {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lane < out[j].Lane })
	return out
}

// recentEvents are the newest stop failures and model switches inside the window.
func recentEvents(in Input) []EventNote {
	from := in.Now.Add(-in.window())
	out := []EventNote{}
	for _, e := range in.Hooks {
		if (e.Kind == EventStopFailure || e.Kind == EventModelSwitch) && !e.At.Before(from) {
			out = append(out, EventNote{At: e.At.UTC(), Kind: e.Kind, Lane: e.Lane, Session: e.Session, Detail: e.Detail})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if n := len(out); n > maxEvents {
		out = out[n-maxEvents:]
	}
	return out
}
