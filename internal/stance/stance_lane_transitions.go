package stance

import "time"

// EnterRequest asks a lane to enter a stance.
type EnterRequest struct {
	Stance  Stance
	Trigger Trigger
	Why     string
	Item    string
	Session string
}

// Skip reasons of a refused automatic entry (never an error: a refusal is the
// normal way "nothing happens").
const (
	SkipMuted    = "muted"
	SkipActive   = "already in this stance"
	SkipBusy     = "in another stance"
	SkipCooldown = "cooldown"
	SkipCap      = "activation cap"
)

// Settle ends an active stance whose time box has run out and returns the exit
// event, or nil. Callers run it before reading or changing a lane, so a lane
// never stays in a stance past exit.timebox_minutes.
func (st *State) Settle(now time.Time) *Event {
	if st.Active == nil || st.Active.Expires.IsZero() || now.Before(st.Active.Expires) {
		return nil
	}
	return st.leave(ExitTimebox, now)
}

// Enter puts the lane into r.Stance and returns the events to log (an exit of
// the stance it replaces, then the enter), or a skip reason and no events.
//
// TriggerManual (`heavyq stance enter`) is explicit: it ignores mute, cooldown
// and cap, unmutes the lane and replaces any stance. A keyword may replace
// another stance too but obeys all three limits: the owner asking twice in a
// row should not re-inject the same text. Evidence never interrupts a lane that
// is already in a stance. Settle has been called.
func (st *State) Enter(r EnterRequest, now time.Time) (events []Event, skip string) {
	explicit := r.Trigger == TriggerManual
	name := r.Stance.Name
	if !explicit {
		switch {
		case st.Muted:
			return nil, SkipMuted
		case st.Active != nil && st.Active.Stance == name:
			return nil, SkipActive
		case st.Active != nil && r.Trigger != TriggerKeyword:
			return nil, SkipBusy
		}
		if cd := time.Duration(r.Stance.CooldownMinutes) * time.Minute; cd > 0 && now.Sub(st.Last[name]) < cd && !st.Last[name].IsZero() {
			return nil, SkipCooldown
		}
		if st.Session == r.Session && st.Counts[name] >= MaxActivationsPerSession {
			return nil, SkipCap
		}
	}
	st.Muted = false
	if st.Active != nil {
		events = append(events, *st.leave(ExitReplaced, now))
	}
	if st.Session != r.Session || st.Counts == nil {
		st.Session, st.Counts = r.Session, map[string]int{}
	}
	st.Counts[name]++
	a := &Active{Stance: name, Trigger: r.Trigger, Why: r.Why, Item: r.Item, Session: r.Session, Since: now.UTC()}
	if m := r.Stance.Exit.TimeboxMinutes; m > 0 {
		a.Expires = a.Since.Add(time.Duration(m) * time.Minute)
	}
	st.Active = a
	st.setLast(name, now)
	return append(events, Event{At: now.UTC(), Op: OpEnter, Lane: st.Lane, Session: r.Session, Stance: name,
		Trigger: r.Trigger, Why: r.Why, Item: r.Item}), ""
}

// Exit leaves the active stance for reason and returns the event, or nil when
// the lane is in none. Whether leaving is allowed is CheckExit's question.
func (st *State) Exit(reason string, now time.Time) *Event { return st.leave(reason, now) }

// Off leaves the active stance (if any) and mutes the lane. It returns the
// events to log.
func (st *State) Off(session string, now time.Time) []Event {
	var events []Event
	if e := st.leave(ExitOff, now); e != nil {
		events = append(events, *e)
	}
	st.Muted = true
	return append(events, Event{At: now.UTC(), Op: OpOff, Lane: st.Lane, Session: session})
}

func (st *State) leave(reason string, now time.Time) *Event {
	a := st.Active
	if a == nil {
		return nil
	}
	st.Active = nil
	st.setLast(a.Stance, now)
	return &Event{At: now.UTC(), Op: OpExit, Lane: st.Lane, Session: a.Session, Stance: a.Stance, Item: a.Item, Reason: reason}
}

func (st *State) setLast(name string, now time.Time) {
	if st.Last == nil {
		st.Last = map[string]time.Time{}
	}
	st.Last[name] = now.UTC()
}
