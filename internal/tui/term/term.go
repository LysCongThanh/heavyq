// Package term owns the terminal for a full-screen program: raw mode, the
// alternate screen, and giving the terminal back in every way a program ends.
//
// The one rule: whatever happens, the user's shell gets a usable terminal.
// Restore runs on a normal exit, on SIGINT/SIGTERM/SIGHUP (reported as a Quit
// event so the caller can unwind), around Ctrl-Z and SIGTSTP/SIGCONT, and
// when a panic unwinds through the caller's deferred Restore. It is safe to
// call more than once.
package term

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"

	xterm "golang.org/x/term"
)

// Control sequences. Frames are wrapped in BSU/ESU (DEC mode 2026,
// "synchronized output"): terminals that know it paint the frame at once,
// the others ignore it.
const (
	// StartReset puts the terminal in a known state before the program draws:
	// attributes off, mouse reporting and bracketed paste off, normal cursor keys.
	StartReset = "\x1b[0m\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[?1l"
	// EnterScreen: alternate screen, hidden cursor, no auto-wrap (a line one
	// cell too long must not scroll the frame), cleared.
	EnterScreen = "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J\x1b[H"
	// LeaveScreen undoes EnterScreen and resets attributes.
	LeaveScreen = "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
	// BSU and ESU begin and end a synchronized update.
	BSU = "\x1b[?2026h"
	ESU = "\x1b[?2026l"
)

// Size is the terminal size in cells.
type Size struct{ Cols, Rows int }

// Sane limits: a resize report outside them is a glitch (a terminal that
// reports 0x0 while it is being torn down, or a corrupted ioctl), not a size.
const (
	maxCols = 1000
	maxRows = 500
)

// SaneSize reports whether cols x rows is a size worth laying out for.
func SaneSize(cols, rows int) bool {
	return cols >= 1 && rows >= 1 && cols <= maxCols && rows <= maxRows
}

// ErrNotTerminal is returned by Start when input or output is not a terminal.
var ErrNotTerminal = errors.New("standard input and output must be a terminal")

// EventKind says what an Event reports.
type EventKind int

// Event kinds.
const (
	EvResize EventKind = iota // the window changed; Size is the new, sane size
	EvQuit                    // SIGINT, SIGTERM or SIGHUP arrived
	EvResume                  // the program continued after a stop: repaint everything
)

// Event is something the terminal or the OS did.
type Event struct {
	Kind EventKind
	Size Size   // EvResize, EvResume
	Sig  string // EvQuit: the signal's name
}

// Session is a terminal in raw mode on the alternate screen.
type Session struct {
	in, out *os.File
	fd      int

	mu     sync.Mutex
	state  *xterm.State
	active bool // between enter and leave: writes reach the screen
	closed bool // Restore ran: enter refuses, so a late SIGCONT cannot re-take the terminal
	last   Size

	events chan Event
	done   chan struct{}
	stop   sync.Once
	sigs   *signals
}

// Start puts in/out into raw mode on the alternate screen. It resets the
// terminal state first, so a previous program's leftovers (mouse reporting,
// bracketed paste) do not leak into the keys we read.
func Start(in, out *os.File) (*Session, error) {
	if !xterm.IsTerminal(int(in.Fd())) || !xterm.IsTerminal(int(out.Fd())) {
		return nil, ErrNotTerminal
	}
	s := &Session{in: in, out: out, fd: int(in.Fd()), events: make(chan Event, 16), done: make(chan struct{})}
	if err := s.enter(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.last = s.measureLocked()
	s.mu.Unlock()
	s.sigs = watchSignals(s)
	return s, nil
}

// enter switches to raw mode and the alternate screen.
func (s *Session) enter() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("terminal already restored")
	}
	st, err := xterm.MakeRaw(s.fd)
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	if s.state == nil {
		s.state = st // keep the very first state: it is the user's
	}
	s.active = true
	_, err = s.out.WriteString(StartReset + EnterScreen)
	return err
}

// leave restores the terminal. It reports nothing: there is no one to tell.
func (s *Session) leave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	s.active = false
	_, _ = s.out.WriteString(LeaveScreen)
	if s.state != nil {
		_ = xterm.Restore(s.fd, s.state)
	}
}

// Restore gives the terminal back and stops watching signals. Idempotent;
// defer it right after Start so a panic still restores.
func (s *Session) Restore() {
	s.stop.Do(func() {
		if s.sigs != nil {
			s.sigs.stop()
		}
		close(s.done)
	})
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.leave()
}

// Events delivers resizes, quit signals and resumes.
func (s *Session) Events() <-chan Event { return s.events }

// Write sends bytes to the screen; it drops them while the terminal is
// handed back (suspended or restored). One call is one write(2) of the whole
// buffer, so a frame is never interleaved with anything else.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return len(p), nil
	}
	return s.out.Write(p)
}

// Size is the current terminal size (the last sane one when it is unreadable).
func (s *Session) Size() Size {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.measureLocked(); SaneSize(m.Cols, m.Rows) {
		s.last = m
	}
	return s.last
}

func (s *Session) measureLocked() Size {
	if c, r, err := xterm.GetSize(int(s.out.Fd())); err == nil && SaneSize(c, r) {
		return Size{c, r}
	}
	// Some terminals and multiplexers answer the ioctl late; fall back to
	// $COLUMNS/$LINES, then to the classic 80x24.
	c, _ := strconv.Atoi(os.Getenv("COLUMNS"))
	r, _ := strconv.Atoi(os.Getenv("LINES"))
	if SaneSize(c, r) {
		return Size{c, r}
	}
	if SaneSize(s.last.Cols, s.last.Rows) {
		return s.last
	}
	return Size{80, 24}
}

// emit delivers e unless the session is over.
func (s *Session) emit(e Event) {
	select {
	case s.events <- e:
	case <-s.done:
	}
}
