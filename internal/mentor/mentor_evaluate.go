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
// normal answer.
func (s *Store) Evaluate(in Input, c Config) (*Decision, error) {
	st := s.LoadLane(in.Lane)
	dirty, err := s.resolvePending(st, in, c)
	if err != nil {
		return nil, err
	}
	d, fresh, err := s.consider(st, in, c)
	if err != nil {
		return nil, err
	}
	if dirty || fresh {
		if err := s.SaveLane(in.Lane, st); err != nil {
			return nil, err
		}
	}
	if d != nil {
		if err := s.AppendEntry(d.Entry); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// resolvePending settles the pending events of the lane that the records can
// settle, appending the resolved entries to the log.
func (s *Store) resolvePending(st *LaneState, in Input, c Config) (changed bool, err error) {
	if len(st.Pending) == 0 {
		return false, nil
	}
	var keep []Pending
	resolved := 0
	for _, p := range st.Pending {
		out := ResolveOutcome(p.Entry, in.Records)
		if out == OutcomePending {
			keep = append(keep, p)
			continue
		}
		e := p.Entry
		e.Outcome, e.OutcomeAt = out, in.Now.UTC()
		if err := s.AppendEntry(e); err != nil {
			return changed, err
		}
		resolved++
		changed = true
	}
	st.Pending = keep
	if resolved > 0 {
		if _, err := s.EvaluateAutoMute(c.AutoMute, in.Now); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// consider runs the rules on the newest record. fresh says that the state
// moved (the record was taken as looked at), so the caller saves it.
func (s *Store) consider(st *LaneState, in Input, c Config) (d *Decision, fresh bool, err error) {
	if len(in.Records) == 0 {
		return nil, false, nil
	}
	newest := in.Records[len(in.Records)-1]
	if newest.ID == "" || newest.ID == st.LastRecord || in.Now.Sub(newest.End) > c.Window {
		return nil, false, nil
	}
	st.LastRecord = newest.ID
	sig, ok := DetectRepeatFailure(in.Records)
	if !ok || sig.Event != newest.ID || s.IsMuted(sig.Rule) || !st.Admit(sig, in.Session, c.Limits) {
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

// SweepPending settles pending events across every lane's records, for
// `heavyq mentor stats`: an event whose lane has not run since would show as
// pending for good. laneRecords returns the records of a lane (oldest first).
// It returns how many events it settled.
func (s *Store) SweepPending(laneRecords func(lane string) ([]history.Record, error), c Config, now time.Time) (int, error) {
	entries, err := s.ReadEntries()
	if err != nil {
		return 0, err
	}
	cache := map[string][]history.Record{}
	n := 0
	for _, e := range entries {
		if e.Outcome != OutcomePending {
			continue
		}
		recs, ok := cache[e.Lane]
		if !ok {
			if recs, err = laneRecords(e.Lane); err != nil {
				return n, err
			}
			cache[e.Lane] = recs
		}
		out := ResolveOutcome(e, recs)
		if out == OutcomePending {
			continue
		}
		e.Outcome, e.OutcomeAt = out, now.UTC()
		if err := s.AppendEntry(e); err != nil {
			return n, err
		}
		n++
	}
	if n > 0 {
		_, err = s.EvaluateAutoMute(c.AutoMute, now)
	}
	return n, err
}
