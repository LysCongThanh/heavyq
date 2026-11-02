package term

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// resizeDelay is how long a burst of SIGWINCH (a window being dragged) is
// collapsed into one resize event.
const resizeDelay = 75 * time.Millisecond

// Debouncer runs fn once, delay after the last Trigger of a burst.
type Debouncer struct {
	delay time.Duration
	fn    func()
	mu    sync.Mutex
	timer *time.Timer
}

// NewDebouncer returns a Debouncer for fn.
func NewDebouncer(delay time.Duration, fn func()) *Debouncer {
	return &Debouncer{delay: delay, fn: fn}
}

// Trigger (re)starts the wait.
func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.fn)
}

// Stop cancels a pending call.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
}

// signals routes OS signals into session events.
type signals struct {
	ch       chan os.Signal
	debounce *Debouncer
	quit     chan struct{}
}

// watchSignals starts the goroutine that turns signals into events:
//
//	SIGINT, SIGTERM, SIGHUP   EvQuit (raw mode has no ISIG, so Ctrl-C is a key, not a signal;
//	                          these come from kill(1), a closing terminal or a supervisor)
//	SIGWINCH                  debounced, sanity-checked EvResize
//	SIGTSTP                   hand the terminal back, stop; on SIGCONT take it again
//	SIGCONT                   re-enter raw mode and the alternate screen: EvResume
func watchSignals(s *Session) *signals {
	w := &signals{ch: make(chan os.Signal, 8), quit: make(chan struct{})}
	w.debounce = NewDebouncer(resizeDelay, func() { s.resized() })
	signal.Notify(w.ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH, syscall.SIGTSTP, syscall.SIGCONT)
	go func() {
		for {
			select {
			case <-w.quit:
				return
			case sig := <-w.ch:
				switch sig {
				case syscall.SIGWINCH:
					w.debounce.Trigger()
				case syscall.SIGTSTP:
					s.Suspend()
				case syscall.SIGCONT:
					s.resumed()
				default:
					s.emit(Event{Kind: EvQuit, Sig: sig.String()})
				}
			}
		}
	}()
	return w
}

func (w *signals) stop() {
	signal.Stop(w.ch)
	w.debounce.Stop()
	close(w.quit)
}

// resized emits a resize when the new size is sane and differs from the last.
func (s *Session) resized() {
	s.mu.Lock()
	m := s.measureLocked()
	changed := SaneSize(m.Cols, m.Rows) && m != s.last
	if changed {
		s.last = m
	}
	s.mu.Unlock()
	if changed {
		s.emit(Event{Kind: EvResize, Size: m})
	}
}

// Suspend hands the terminal back and stops the process (what Ctrl-Z does in
// a cooked terminal). SIGSTOP, unlike SIGTSTP, cannot be caught, so the
// process really stops; execution continues here after SIGCONT, which the
// signal goroutine turns into resumed().
func (s *Session) Suspend() {
	s.leave()
	_ = syscall.Kill(os.Getpid(), syscall.SIGSTOP)
}

// resumed re-enters raw mode and the alternate screen and asks for a repaint.
func (s *Session) resumed() {
	select {
	case <-s.done:
		return
	default:
	}
	if err := s.enter(); err != nil {
		return // the terminal is gone; a SIGHUP will follow
	}
	s.mu.Lock()
	m := s.measureLocked()
	s.last = m
	s.mu.Unlock()
	s.emit(Event{Kind: EvResume, Size: m})
}
