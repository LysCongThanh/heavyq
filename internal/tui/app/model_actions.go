package app

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/input"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

const readOnlyHint = "read-only: restart with --control to act"

// act runs c, +, - or p on the selected row. Without --control it only says
// how to enable them; with it, a cancel asks y/n first and the rest go at once.
func (m *Model) act(cmd input.Command, now time.Time) {
	if m.control == nil {
		m.say(now, readOnlyHint)
		return
	}
	r, ok := m.selected()
	if !ok {
		return
	}
	switch cmd {
	case input.CmdCancel:
		m.cancel(r, now)
	case input.CmdRaise, input.CmdLower:
		m.reprio(r, cmd == input.CmdRaise, now)
	case input.CmdPause:
		m.pause(r, now)
	}
}

// ticketOf is the ticket an action on r applies to, and a label for it.
func ticketOf(r render.Row) (id, label string, ok bool) {
	switch {
	case r.Kind == render.RowTicket:
		return r.Ticket, r.Lane + " (" + short(r.Cmd) + ")", true
	case r.Exception != nil && r.Exception.Action != nil && r.Exception.Action.Kind != snapshot.ActionRun &&
		r.Exception.Action.Kind != snapshot.ActionResume:
		return r.Exception.Action.Target, r.Lane, true
	}
	return "", "", false
}

func (m *Model) cancel(r render.Row, now time.Time) {
	if r.Exception != nil && (r.Exception.Action == nil || r.Exception.Action.Kind != snapshot.ActionCancel) {
		m.say(now, "nothing to cancel on this row")
		return
	}
	id, label, ok := ticketOf(r)
	if !ok || id == "" {
		m.say(now, "select a job to cancel")
		return
	}
	m.ask = &confirm{prompt: "cancel " + label + "?", done: "cancel requested for " + r.Lane,
		do: func() error { return m.control.Cancel(id) }}
}

func (m *Model) reprio(r render.Row, raise bool, now time.Time) {
	id, _, ok := ticketOf(r)
	if !ok || id == "" || (r.Kind == render.RowTicket && !r.Waiting) {
		m.say(now, "only a waiting job changes priority")
		return
	}
	prio := r.Prio
	if r.Exception != nil { // the exception already names the priority it asks for
		if !raise {
			m.say(now, "use + on this row")
			return
		}
		prio = r.Exception.Action.Value
	} else if raise {
		prio--
	} else {
		prio++
	}
	if prio < 1 || prio > 9 {
		m.say(now, "priority stays at %d (limits are 1 and 9)", r.Prio)
		return
	}
	if err := m.control.Prio(id, prio); err != nil {
		m.say(now, "failed: %v", err)
		return
	}
	m.say(now, "%s asked to move to p%d", r.Lane, prio)
}

func (m *Model) pause(r render.Row, now time.Time) {
	if r.Machine == "" || m.snap == nil {
		m.say(now, "select a machine, or a job running on one")
		return
	}
	paused := false
	for _, mc := range m.snap.Machines {
		if mc.Name == r.Machine {
			paused = mc.Paused
		}
	}
	if err := m.control.SetPaused(r.Machine, !paused); err != nil {
		m.say(now, "failed: %v", err)
		return
	}
	verb := "paused"
	if paused {
		verb = "resumed"
	}
	m.say(now, "%s %s", r.Machine, verb)
}
