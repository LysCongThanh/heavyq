package mentor

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Config is the tuning of Evaluate.
type Config struct {
	// HoldoutPct is the share of eligible events held out, 0-100.
	HoldoutPct int
	Limits     Limits
	AutoMute   AutoMute
	// Window is how recent the lane's newest record must be for the call to
	// count as the end of that run: the hook fires right after the tool call.
	Window time.Duration
}

// DefaultConfig is the research design: 80/20, 3 per session, auto-mute on.
func DefaultConfig() Config {
	return Config{HoldoutPct: DefaultHoldoutPct, Limits: DefaultLimits, AutoMute: DefaultAutoMute, Window: time.Minute}
}

// Input is what the hook knows: the lane, the agent session, the lane's
// records from the ledger index (oldest first) and the time.
type Input struct {
	Lane    string
	Session string
	Records []history.Record
	Now     time.Time
}

// Decision is an eligible event. Message is the note to show, empty for the
// holdout arm: the event is logged either way.
type Decision struct {
	Entry   Entry
	Message string
}

// Evaluate is the whole mentor for one hook call. In order:
//
//  1. events of this lane awaiting an outcome are resolved from the records,
//     and a resolution may auto-mute a rule;
//  2. the newest record is considered once (never twice, never when it is
//     older than Window), by every rule that is not muted;
//  3. a signal must pass the lane's state (state change only, cooldown, cap),
//     and then the arm is drawn from the seed;
//  4. the event is logged, whichever arm, and only then returned.
//
// Any error means "say nothing": the caller fails open. A nil Decision is the
// normal answer. Evaluate is Plan then Commit; a caller under a deadline runs
// Plan under it and Commits only when the answer came in time.
func (s *Store) Evaluate(in Input, c Config) (*Decision, error) {
	p, err := s.Plan(in, c)
	if err != nil {
		return nil, err
	}
	if err := s.Commit(p, in.Now); err != nil {
		return nil, err
	}
	return p.Decision, nil
}

// Plan is what one call of the mentor decided, before anything is written:
// a note that missed its deadline is never shown, so none of it may be
// recorded either (an inject event the agent never saw would bias the lift,
// and a record taken as looked at would never be said).
type Plan struct {
	Decision *Decision
	lane     string
	state    *LaneState
	save     bool
	resolved []Entry    // pending events the records settled
	mutes    []autoMute // rules the settled events auto-mute
}

// Plan decides steps 1-3 of Evaluate without writing anything (except, once
// ever, the seed: it is stable).
func (s *Store) Plan(in Input, c Config) (*Plan, error) {
	p := &Plan{lane: in.Lane, state: s.LoadLane(in.Lane)}
	p.resolved = settlePending(p.state, in)
	mutes := s.Mutes()
	if len(p.resolved) > 0 && c.AutoMute.Enabled {
		entries, err := s.ReadEntries()
		if err != nil {
			return nil, err
		}
		p.mutes = autoMutes(c.AutoMute, append(entries, p.resolved...), mutes)
		for _, m := range p.mutes {
			mutes[m.rule] = MuteEntry{Muted: true, By: MutedByAuto}
		}
	}
	d, fresh, err := s.consider(p.state, in, c, mutes)
	if err != nil {
		return nil, err
	}
	p.Decision, p.save = d, fresh || len(p.resolved) > 0
	return p, nil
}

// Commit writes what p decided: the settled events, the auto-mutes, the lane
// state and the new event.
func (s *Store) Commit(p *Plan, now time.Time) error {
	for _, e := range p.resolved {
		if err := s.AppendEntry(e); err != nil {
			return err
		}
	}
	for _, m := range p.mutes {
		if err := s.SetMute(m.rule, true, MutedByAuto, m.why, now); err != nil {
			return err
		}
	}
	if p.save {
		if err := s.SaveLane(p.lane, p.state); err != nil {
			return err
		}
	}
	if p.Decision != nil {
		return s.AppendEntry(p.Decision.Entry)
	}
	return nil
}

// settlePending takes the pending events of the lane that the records settle
// out of st and returns them, resolved.
func settlePending(st *LaneState, in Input) []Entry {
	var keep []Pending
	var resolved []Entry
	for _, p := range st.Pending {
		out := ResolveOutcome(p.Entry, in.Records)
		if out == OutcomePending {
			keep = append(keep, p)
			continue
		}
		e := p.Entry
		e.Outcome, e.OutcomeAt = out, in.Now.UTC()
		resolved = append(resolved, e)
	}
	st.Pending = keep
	return resolved
}

// consider runs the rules on the newest record. fresh says that the state
// moved (the record was taken as looked at), so the caller saves it. mutes is
// the mute state to judge by (including auto-mutes not yet written).
func (s *Store) consider(st *LaneState, in Input, c Config, mutes map[string]MuteEntry) (d *Decision, fresh bool, err error) {
	if len(in.Records) == 0 {
		return nil, false, nil
	}
	newest := in.Records[len(in.Records)-1]
	if newest.ID == "" || newest.ID == st.LastRecord || in.Now.Sub(newest.End) > c.Window {
		return nil, false, nil
	}
	st.LastRecord = newest.ID
	sig, ok := DetectRepeatFailure(in.Records)
	if !ok || sig.Event != newest.ID || mutes[sig.Rule].Muted || !st.Admit(sig, in.Session, c.Limits) {
		return nil, true, nil
	}
	seed, err := s.Seed()
	if err != nil {
		return nil, true, err
	}
	e := Entry{At: in.Now.UTC(), Rule: sig.Rule, Level: sig.Level, Seed: seed,
		Arm: Assign(seed, sig.Rule, sig.Event, c.HoldoutPct), Event: sig.Event, Lane: in.Lane,
		Session: in.Session, RunID: sig.Signature, Tree: sig.Tree, Streak: sig.Streak, Outcome: OutcomePending}
	st.Pending = append(st.Pending, Pending{Entry: e})
	st.trim()
	d = &Decision{Entry: e}
	if e.Arm == ArmInject {
		d.Message = RenderMessage(sig)
	}
	return d, true, nil
}
