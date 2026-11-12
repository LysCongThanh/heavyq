package queue

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
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
//
// Each attempt goes through slots.TryLock, which opens .mutex at its path
// again and checks after flock that it holds the file still there. A waiter
// that kept retrying on a .mutex a temp cleaner unlinked meanwhile would
// otherwise win a lock nobody else can see, and two rounds would run at once
// on two different files. The held .mutex is kept fresh like a slot file.
func (d *Dir) Mutex(wait time.Duration, stop func() bool) (func(), error) {
	path := filepath.Join(d.Path, ".mutex")
	deadline := time.Now().Add(wait)
	pause := time.Millisecond
	for {
		l, err := slots.TryLock(path)
		switch {
		case err != nil:
			return nil, fmt.Errorf("queue mutex: %w", err)
		case l != nil:
			return l.Release, nil
		}
		if (stop != nil && stop()) || !time.Now().Before(deadline) {
			return nil, ErrMutexBusy
		}
		// Jitter keeps the waiters of a busy queue from retrying in lockstep.
		time.Sleep(pause + time.Duration(rand.Int64N(int64(pause))))
		pause = min(2*pause, maxMutexPause)
	}
}
