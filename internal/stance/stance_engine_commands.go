package stance

import (
	"fmt"

	"github.com/LysCongThanh/heavyq/internal/lease"
)

// EnterManual is `heavyq stance enter <name>`: an explicit request, so it
// ignores mute, cooldown and cap. It returns the compact render to print.
// A stance of an untrusted repo is refused with the way out.
func (e Env) EnterManual(cwd, lane, session, item, name, why string) (*Entry, error) {
	set, err := e.LoadSet(cwd)
	if err != nil {
		return nil, err
	}
	s, ok := Find(set.Injectable, name)
	if !ok {
		if all, found := Find(set.All, name); found && set.Untrusted(all) {
			return nil, fmt.Errorf("stance %q comes from this repository's .heavyq/stances, which you have not trusted; read the files, then run: heavyq trust", name)
		}
		return nil, fmt.Errorf("no stance named %q", name)
	}
	if item != "" {
		if err := lease.ValidateItem(item); err != nil {
			return nil, err
		}
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return nil, err
	}
	now := e.now()
	if item == "" {
		item = LaneItem(set.Common, lane, now)
	}
	if why = oneLine(why); why == "" {
		why = "you asked for it (heavyq stance enter)"
	}
	var entry *Entry
	err = store.Update(lane, func(st *State) ([]Event, error) {
		var events []Event
		if ex := st.Settle(now); ex != nil {
			events = append(events, *ex)
		}
		evs, _ := st.Enter(EnterRequest{Stance: s, Trigger: TriggerManual, Why: why, Item: item, Session: session}, now)
		entry = &Entry{Stance: s.Name, Trigger: TriggerManual, Why: why, Context: Render(s, why)}
		return append(events, evs...), nil
	})
	return entry, err
}

// LeaveResult is what `heavyq stance exit` found and did.
type LeaveResult struct {
	Stance string
	Left   bool      // the lane left the stance
	Reason string    // why it left: one of the Exit* reasons
	Check  ExitCheck // the verdict (also when forced)
	Record string    // the decision record path that was checked ("" when none is needed)
}

// Leave is `heavyq stance exit`: it checks the exit condition of the lane's
// stance (CheckExit) and leaves when it is met, or when force is set, which is
// logged as such. Without an active stance it is an error. item overrides the
// item whose decision record is read.
func (e Env) Leave(cwd, lane, item string, force bool) (LeaveResult, error) {
	var res LeaveResult
	set, err := e.LoadSet(cwd)
	if err != nil {
		return res, err
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return res, err
	}
	now := e.now()
	err = store.Update(lane, func(st *State) ([]Event, error) {
		var events []Event
		if ex := st.Settle(now); ex != nil {
			events = append(events, *ex)
		}
		if st.Active == nil {
			return events, fmt.Errorf("lane %q is in no stance", lane)
		}
		res.Stance = st.Active.Stance
		s, _ := Find(set.All, res.Stance) // a deleted stance has no exit condition left
		if item == "" {
			item = st.Active.Item
		}
		var record []byte
		exists := false
		if s.Exit.DecisionRecord {
			top, err := workTreeTop(cwd)
			if err != nil {
				return events, err
			}
			if item != "" {
				if err := lease.ValidateItem(item); err != nil {
					return events, err
				}
			}
			res.Record = DecisionPath(top, DecisionKey(item, lane))
			if record, exists, err = ReadDecision(res.Record); err != nil {
				return events, err
			}
		}
		res.Check = CheckExit(s, *st.Active, record, exists, now)
		reason := res.Check.Reason
		switch {
		case res.Check.Allowed:
		case force:
			reason = ExitForced
		default:
			return events, nil
		}
		if ex := st.Exit(reason, now); ex != nil {
			events = append(events, *ex)
			res.Left, res.Reason = true, reason
		}
		return events, nil
	})
	return res, err
}

// Off is `heavyq stance off`: leave the lane's stance and mute the lane until
// the next `heavyq stance enter`.
func (e Env) Off(cwd, lane, session string) error {
	set, err := e.LoadSet(cwd)
	if err != nil {
		return err
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return err
	}
	now := e.now()
	return store.Update(lane, func(st *State) ([]Event, error) {
		var events []Event
		if ex := st.Settle(now); ex != nil {
			events = append(events, *ex)
		}
		return append(events, st.Off(session, now)...), nil
	})
}
