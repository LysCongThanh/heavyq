package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// mutexWait bounds one attempt on the queue mutex. A holder keeps it for a
// round, milliseconds; a waiter that cannot get it in this long skips the round
// (its ticket stays) and tries again after a poll.
const mutexWait = 10 * time.Second

// catchStops holds Ctrl-Z (SIGTSTP) back while this process is in a scheduling
// round: stopped there, it would keep the queue mutex and stall every other
// waiter's rounds until it is resumed. Outside a round the stop happens at
// once. It stops with SIGSTOP, which cannot be caught (as heavyq top does):
// once os/signal has caught SIGTSTP, raising it again would only be caught
// again. The returned func undoes it.
func (r *run) catchStops() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTSTP)
	go func() {
		for range ch {
			r.stopAsked.Store(true) // a round still waiting for the mutex gives up
			r.inRound.Lock()
			r.stopAsked.Store(false)
			_ = syscall.Kill(os.Getpid(), syscall.SIGSTOP) // execution continues here after SIGCONT
			r.inRound.Unlock()
		}
	}()
	return func() { signal.Stop(ch); close(ch) }
}

// stopWaiting reports whether a waiter should stop waiting for the queue
// mutex: a stop or a signal to handle, an operator cancel or --max-wait is due.
func (r *run) stopWaiting() bool {
	return r.stopAsked.Load() || len(r.sigs) > 0 || controlBox().CancelRequested(r.me.Name) ||
		(r.opts.MaxWait > 0 && time.Since(r.t0) >= r.opts.MaxWait)
}

// warnMutexBusy tells a person (not an agent: its output is a budget) why the
// wait does not move when another heavyq keeps the queue mutex for a whole
// attempt, at most once per downWarnEvery.
func (r *run) warnMutexBusy(tried time.Duration, lastWarn *time.Time) {
	now := time.Now()
	if tried < mutexWait || r.out.mode.Terse() || now.Sub(*lastWarn) < downWarnEvery {
		return
	}
	fmt.Fprintf(r.stderr, "heavyq: another heavyq has held the queue lock for over %s (stopped with Ctrl-Z?); "+
		"this run waits until it is resumed or ends\n", fmtDur(mutexWait))
	*lastWarn = now
}
