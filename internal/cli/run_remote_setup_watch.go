package cli

import (
	"context"
	"os"
	"time"
)

// setupWatch stands in for supervise while a remote run ships its commit:
// the first signal or operator cancel request ends the shipping context, so
// the push stops with its ssh at once instead of after the push (or never).
type setupWatch struct {
	stop      chan struct{}
	done      chan struct{}
	sig       os.Signal
	cancelled bool
}

func (r *run) watchSetup(cancel context.CancelFunc) *setupWatch {
	w := &setupWatch{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		tick := time.NewTicker(controlPoll)
		defer tick.Stop()
		for {
			select {
			case <-w.stop:
				return
			case s := <-r.sigs:
				w.sig = s
				cancel()
				return
			case <-tick.C:
				if controlBox().CancelRequested(r.me.Name) {
					w.cancelled = true
					cancel()
					return
				}
			}
		}
	}()
	return w
}

// setupStopped ends the watch. When a signal or a cancel request stopped the
// setup it records why and returns the run's exit code (128+n or 78) with
// stopped true. A signal that comes later waits in r.sigs for supervise.
func (r *run) setupStopped(w *setupWatch) (int, bool) {
	close(w.stop)
	<-w.done
	switch {
	case w.sig != nil:
		r.interrupted = true
		return signalExit(w.sig), true
	case w.cancelled:
		r.cancelled = true
		r.note("cancelled by an operator while the commit was shipped; the command did not run")
		return ExitCancelled, true
	}
	return 0, false
}
