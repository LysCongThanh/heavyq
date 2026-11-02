// Package app is the heavyq top program: a model that turns keys and
// snapshots into view state, a fetcher that builds snapshots without
// overlapping, a painter that writes only changed lines, and the loop that
// ties them to a terminal. Rendering is internal/tui/render, key decoding
// internal/tui/input, terminal control internal/tui/term.
package app

import (
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/input"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// Actions are the control-mailbox requests of --control, the same ones the
// dashboard makes. A nil Actions means read-only.
type Actions interface {
	Cancel(ticket string) error
	Prio(ticket string, prio int) error
	SetPaused(machine string, paused bool) error
}

// flashFor is how long a result line stays in the footer.
const flashFor = 4 * time.Second

// Effect is what the loop must do besides repainting.
type Effect struct {
	Quit, Refresh, Suspend, Redraw bool
	Copy                           string // text for the clipboard (OSC 52)
	Bell                           bool   // a new exception appeared
}

// confirm is an inline y/n question waiting for an answer.
type confirm struct {
	prompt string
	do     func() error
	done   string
}

// Model is the state of the screen. It is not safe for concurrent use: the
// loop owns it.
type Model struct {
	Mode    render.Mode
	control Actions
	why     func(lane string, s snapshot.Snapshot, now time.Time) []string

	snap    *snapshot.Snapshot
	fetched time.Time
	err     string
	seen    map[string]bool // exception ids already announced

	view       render.View
	ask        *confirm
	flash      string
	flashUntil time.Time
}

// NewModel returns a Model. why explains a lane (heavyq why <lane>); control
// is nil for a read-only screen.
func NewModel(mode render.Mode, interval time.Duration, control Actions,
	why func(string, snapshot.Snapshot, time.Time) []string) *Model {
	return &Model{Mode: mode, control: control, why: why, view: render.View{Interval: interval, Control: control != nil}}
}

// Snapshot feeds a build result. It returns an Effect with Bell set when a
// new exception appeared since the last snapshot (never for the first one).
func (m *Model) Snapshot(s snapshot.Snapshot, err error, now time.Time) Effect {
	if err != nil {
		m.err = err.Error()
		return Effect{}
	}
	m.err, m.fetched = "", now
	first := m.snap == nil
	m.snap = &s
	var eff Effect
	ids := map[string]bool{}
	for _, e := range s.Exceptions {
		ids[e.ID] = true
		if !first && !m.seen[e.ID] {
			eff.Bell = true
		}
	}
	m.seen = ids
	m.reselect()
	return eff
}

// View is the render state at now.
func (m *Model) View(now time.Time) render.View {
	v := m.view
	v.Now, v.Fetched, v.Err = now, m.fetched, m.err
	if m.ask != nil {
		v.Prompt = m.ask.prompt
	}
	if m.flash != "" && now.Before(m.flashUntil) {
		v.Flash = m.flash
	}
	return v
}

// Snap is the current snapshot, nil before the first.
func (m *Model) Snap() *snapshot.Snapshot { return m.snap }

func (m *Model) rows() []render.Row {
	if m.snap == nil {
		return nil
	}
	return render.RowsIn(*m.snap, m.view.Filter, m.Mode)
}

// reselect keeps the selection on the same row across refreshes and filter
// changes; when the row is gone it stays at the same position.
func (m *Model) reselect() {
	rows := m.rows()
	if len(rows) == 0 {
		m.view.Selected = ""
		return
	}
	if i := render.Find(rows, m.view.Selected); i >= 0 {
		return
	}
	m.view.Selected = rows[0].Key
}

func (m *Model) selected() (render.Row, bool) {
	rows := m.rows()
	if i := render.Find(rows, m.view.Selected); i >= 0 {
		return rows[i], true
	}
	return render.Row{}, false
}

func (m *Model) say(now time.Time, format string, a ...any) {
	m.flash, m.flashUntil = fmt.Sprintf(format, a...), now.Add(flashFor)
}

// Key handles one key press.
func (m *Model) Key(k input.Key, now time.Time) Effect {
	switch {
	case m.ask != nil:
		return m.answer(k, now)
	case m.view.Typing:
		return m.typing(k)
	case m.view.Overlay != render.OverlayNone:
		return m.overlayKey(k)
	}
	return m.normal(input.Map(k), now)
}

func (m *Model) answer(k input.Key, now time.Time) Effect {
	a := m.ask
	m.ask = nil
	if input.Map(k) != input.CmdYes {
		m.say(now, "not done")
		return Effect{}
	}
	if err := a.do(); err != nil {
		m.say(now, "failed: %v", err)
		return Effect{}
	}
	m.say(now, "%s", a.done)
	return Effect{Refresh: true}
}

func (m *Model) typing(k input.Key) Effect {
	switch k.Kind {
	case input.KeyEnter:
		m.view.Typing = false
	case input.KeyEsc, input.KeyCtrlC:
		m.view.Typing, m.view.Filter = false, ""
	case input.KeyBackspace:
		if r := []rune(m.view.Filter); len(r) > 0 {
			m.view.Filter = string(r[:len(r)-1])
		}
	case input.KeyRune:
		if width.Rune(k.Rune) > 0 && len([]rune(m.view.Filter)) < 64 {
			m.view.Filter += string(k.Rune)
		}
	}
	m.reselect()
	return Effect{}
}

func (m *Model) overlayKey(k input.Key) Effect {
	if input.Map(k) == input.CmdQuit {
		return Effect{Quit: true}
	}
	switch k.Kind {
	case input.KeyEsc, input.KeyEnter, input.KeyRune, input.KeyBackspace:
		m.view.Overlay = render.OverlayNone
	}
	return Effect{}
}
