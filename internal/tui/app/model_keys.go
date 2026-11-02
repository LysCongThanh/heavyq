package app

import (
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/input"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

const pageRows = 8

// normal runs a command in normal mode (no prompt, no typing, no overlay).
func (m *Model) normal(cmd input.Command, now time.Time) Effect {
	switch cmd {
	case input.CmdQuit:
		return Effect{Quit: true}
	case input.CmdSuspend:
		return Effect{Suspend: true}
	case input.CmdRedraw:
		return Effect{Redraw: true}
	case input.CmdRefresh:
		return Effect{Refresh: true}
	case input.CmdHelp:
		m.view.Overlay = render.OverlayHelp
	case input.CmdFilter:
		m.view.Typing = true
	case input.CmdBack:
		m.view.Filter = ""
		m.reselect()
	case input.CmdMode:
		m.Mode = m.Mode.Next()
		m.view.Selected = ""
		m.reselect()
		m.say(now, "mode %s", m.Mode)
	case input.CmdUp, input.CmdDown, input.CmdPageUp, input.CmdPageDown, input.CmdTop, input.CmdBottom,
		input.CmdNextSection, input.CmdPrevSection:
		m.move(cmd)
	case input.CmdWhy:
		m.openWhy(now)
	case input.CmdYes:
		return m.copySelected(now)
	case input.CmdCancel, input.CmdRaise, input.CmdLower, input.CmdPause:
		m.act(cmd, now)
	}
	return Effect{}
}

// move changes the selection.
func (m *Model) move(cmd input.Command) {
	rows := m.rows()
	if len(rows) == 0 {
		return
	}
	i := max(render.Find(rows, m.view.Selected), 0)
	switch cmd {
	case input.CmdUp:
		i--
	case input.CmdDown:
		i++
	case input.CmdPageUp:
		i -= pageRows
	case input.CmdPageDown:
		i += pageRows
	case input.CmdTop:
		i = 0
	case input.CmdBottom:
		i = len(rows) - 1
	case input.CmdNextSection:
		i = sectionStart(rows, i, +1)
	case input.CmdPrevSection:
		i = sectionStart(rows, i, -1)
	}
	m.view.Selected = rows[max(0, min(len(rows)-1, i))].Key
}

// sectionStart is the index of the first row of the next (dir +1) or
// previous (dir -1) section, wrapping around.
func sectionStart(rows []render.Row, from, dir int) int {
	kinds := []render.RowKind{}
	for _, r := range rows {
		if len(kinds) == 0 || kinds[len(kinds)-1] != r.Kind {
			kinds = append(kinds, r.Kind)
		}
	}
	at := 0
	for n, k := range kinds {
		if k == rows[from].Kind {
			at = n
		}
	}
	want := kinds[(at+dir+len(kinds))%len(kinds)]
	for i, r := range rows {
		if r.Kind == want {
			return i
		}
	}
	return from
}

// openWhy shows the explanation of the selected row.
func (m *Model) openWhy(now time.Time) {
	r, ok := m.selected()
	if !ok || m.snap == nil {
		return
	}
	var lines []string
	title := r.Lane
	switch {
	case r.Exception != nil: // the evidence first, then what the lane is doing
		lines, title = exceptionLines(*r.Exception), r.Exception.Kind
		if r.Lane != "" && m.why != nil {
			lines = append(lines, m.why(r.Lane, *m.snap, now)...)
		}
	case r.Mach != nil:
		lines, title = machineLines(*r.Mach), r.Machine
	case r.Lane != "" && m.why != nil:
		lines = m.why(r.Lane, *m.snap, now)
	}
	if len(lines) == 0 {
		return
	}
	m.view.Overlay, m.view.WhyTitle, m.view.WhyLines = render.OverlayWhy, title, lines
}

func exceptionLines(e snapshot.Exception) []string {
	out := []string{e.Text}
	if e.Evidence.Tree != "" {
		out = append(out, "tree "+e.Evidence.Tree)
	}
	if e.Evidence.Cmd != "" {
		out = append(out, "command: "+e.Evidence.Cmd)
	}
	if e.Evidence.Detail != "" {
		out = append(out, e.Evidence.Detail)
	}
	if e.Action != nil {
		out = append(out, "to act: "+e.Action.Command)
	}
	return out
}

func machineLines(mc snapshot.Machine) []string {
	out := []string{mc.Name + ": " + map[bool]string{true: "reachable", false: "unreachable"}[mc.Reachable]}
	if mc.Paused {
		out = append(out, "paused: nothing new starts here")
	}
	if mc.Admits != "" {
		out = append(out, "a heavy job cannot start now: "+mc.Admits)
	} else if mc.Reachable {
		out = append(out, "a heavy job could start here now")
	}
	return out
}

// copySelected asks the loop to put the selected command on the clipboard.
func (m *Model) copySelected(now time.Time) Effect {
	r, ok := m.selected()
	if !ok {
		return Effect{}
	}
	text := r.Cmd
	switch {
	case r.Exception != nil && r.Exception.Action != nil:
		text = r.Exception.Action.Command
	case r.Mach != nil:
		text = r.Machine
	case text == "" && r.Exception != nil:
		text = r.Exception.Text
	}
	if text == "" {
		return Effect{}
	}
	m.say(now, "copied: %s", width.Truncate(text, 40, "…"))
	return Effect{Copy: text}
}

// short is a command line cut for a prompt.
func short(s string) string { return strings.TrimSpace(width.Truncate(s, 32, "…")) }
