package render

import (
	"strings"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// RowKind is the section a selectable row belongs to.
type RowKind int

// Row kinds, in screen order.
const (
	RowException RowKind = iota
	RowTicket
	RowMachine
)

// Row is one selectable line of the screen. The application navigates and
// acts on Rows; Frame draws them. Key is stable across snapshots, so the
// selection survives a refresh.
type Row struct {
	Kind    RowKind
	Key     string
	Lane    string
	Machine string // where it runs, or the machine of a machine row or exception
	Ticket  string // ticket id: of a ticket row, or the target of the exception's action
	Waiting bool   // ticket rows: still in the queue (priority can change)
	Prio    int
	Cmd     string
	Paused  bool // machine rows

	Running   *snapshot.Running
	Wait      *snapshot.Waiting
	Exception *snapshot.Exception
	Mach      *snapshot.Machine
}

// Rows lists what the screen shows as selectable, in order: exceptions, then
// running and waiting tickets, then machines. A non-empty filter keeps the
// exceptions and tickets whose lane, machine or command contain it (case
// insensitive); machines always stay.
func Rows(s snapshot.Snapshot, filter string) []Row {
	f := strings.ToLower(strings.TrimSpace(filter))
	match := func(parts ...string) bool {
		if f == "" {
			return true
		}
		return strings.Contains(strings.ToLower(strings.Join(parts, " ")), f)
	}
	var rows []Row
	for i := range s.Exceptions {
		e := &s.Exceptions[i]
		if !match(e.Lane, e.Machine, e.Kind, e.Evidence.Cmd, e.Text) {
			continue
		}
		r := Row{Kind: RowException, Key: "x:" + e.ID, Lane: e.Lane, Machine: e.Machine, Exception: e}
		if e.Action != nil {
			r.Ticket = e.Action.Target
			if e.Action.Kind == snapshot.ActionResume {
				r.Machine = e.Action.Target
			}
		}
		rows = append(rows, r)
	}
	for i := range s.Running {
		t := &s.Running[i]
		if match(t.Lane, t.Machine, t.Cmd) {
			rows = append(rows, Row{Kind: RowTicket, Key: "t:" + t.ID, Lane: t.Lane, Machine: t.Machine,
				Ticket: t.ID, Prio: t.Prio, Cmd: t.Cmd, Running: t})
		}
	}
	for i := range s.Waiting {
		t := &s.Waiting[i]
		if match(t.Lane, t.Target, t.Cmd) {
			rows = append(rows, Row{Kind: RowTicket, Key: "t:" + t.ID, Lane: t.Lane, Ticket: t.ID,
				Waiting: true, Prio: t.Prio, Cmd: t.Cmd, Wait: t})
		}
	}
	for i := range s.Machines {
		m := &s.Machines[i]
		rows = append(rows, Row{Kind: RowMachine, Key: "m:" + m.Name, Machine: m.Name, Paused: m.Paused, Mach: m})
	}
	return rows
}

// Find is the index of the row with key, or -1.
func Find(rows []Row, key string) int {
	for i, r := range rows {
		if r.Key == key {
			return i
		}
	}
	return -1
}
