package queue

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrMutexBusy means the queue mutex stayed taken for the whole wait, or the
// caller asked to stop waiting. The ticket is untouched: try again next round.
var ErrMutexBusy = errors.New("queue mutex busy")

// maxMutexPause bounds the pause between two attempts on a busy mutex.
const maxMutexPause = 50 * time.Millisecond

// Mutex serialises scheduling decisions across all heavyq processes. The
// returned func releases it.
//
// It polls instead of blocking in flock(2): a holder that is stopped (Ctrl-Z,
// SIGSTOP, a debugger) or hung would otherwise hang every waiter, deaf to
// signals, cancel and --max-wait (a signal caught by os/signal does not
// interrupt a blocking flock). It gives up with ErrMutexBusy after wait, or as
// soon as stop (which may be nil) reports true.
func (d *Dir) Mutex(wait time.Duration, stop func() bool) (func(), error) {
	f, err := os.OpenFile(filepath.Join(d.Path, ".mutex"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("queue mutex: %w", err)
	}
	deadline := time.Now().Add(wait)
	pause := time.Millisecond
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return func() { _ = f.Close() }, nil
		case errors.Is(err, syscall.EINTR):
			continue
		case !errors.Is(err, syscall.EWOULDBLOCK):
			f.Close()
			return nil, fmt.Errorf("queue mutex: %w", err)
		}
		if (stop != nil && stop()) || !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrMutexBusy
		}
		// Jitter keeps the waiters of a busy queue from retrying in lockstep.
		time.Sleep(pause + time.Duration(rand.Int64N(int64(pause))))
		pause = min(2*pause, maxMutexPause)
	}
}
