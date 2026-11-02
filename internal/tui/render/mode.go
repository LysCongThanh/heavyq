package render

import "github.com/LysCongThanh/heavyq/internal/snapshot"

// Mode is a display mode: what the screen shows.
type Mode int

// Modes, in the order the m key cycles through them.
const (
	ModeFull     Mode = iota // exceptions, lanes, machines, story; a right column from 140 columns
	ModeFocus                // only what needs you, and one summary line: for a tiny pane
	ModeLanes                // the lane table with every column: where, wait, run, gate tree, item, command
	ModeMachines             // machines, pools and what runs outside the queue
	ModeLine                 // a single-line strip
)

var modeNames = [...]string{"full", "focus", "lanes", "machines", "line"}

// ModeNames lists the modes for usage messages.
func ModeNames() []string { return modeNames[:] }

func (m Mode) String() string {
	if m < 0 || int(m) >= len(modeNames) {
		return "full"
	}
	return modeNames[m]
}

// ParseMode reads a mode name.
func ParseMode(s string) (Mode, bool) {
	for i, n := range modeNames {
		if n == s {
			return Mode(i), true
		}
	}
	return ModeFull, false
}

// Next is the mode after m, wrapping around.
func (m Mode) Next() Mode { return Mode((int(m) + 1) % len(modeNames)) }

// RowsIn is Rows limited to what mode m shows, so the selection never rests
// on a line that is not on screen.
func RowsIn(s snapshot.Snapshot, filter string, m Mode) []Row {
	all := Rows(s, filter)
	var keep func(RowKind) bool
	switch m {
	case ModeFocus:
		keep = func(k RowKind) bool { return k == RowException }
	case ModeLanes:
		keep = func(k RowKind) bool { return k == RowTicket }
	case ModeMachines:
		keep = func(k RowKind) bool { return k == RowMachine }
	case ModeLine:
		return nil
	default:
		return all
	}
	var out []Row
	for _, r := range all {
		if keep(r.Kind) {
			out = append(out, r)
		}
	}
	return out
}
