package notify

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Options of Check.
type Options struct {
	Budget int // pushes per day; 0 turns pushing off (the state is still kept)
	// Channels is asked only when there is something to push, so the common
	// case never probes for notifier programs. nil means nothing is delivered.
	Channels func() []Channel
}

// Fixed is a Channels function that returns the given channels.
func Fixed(c ...Channel) func() []Channel { return func() []Channel { return c } }

// BudgetFromEnv reads HEAVYQ_NOTIFY_BUDGET; anything that is not a
// non-negative integer is the default.
func BudgetFromEnv(env Env) int {
	if n, err := strconv.Atoi(env("HEAVYQ_NOTIFY_BUDGET")); err == nil && n >= 0 {
		return n
	}
	return DefaultBudget
}

// Dir is where the dedupe state of the repository with this git common dir lives.
func Dir(commonDir string) string { return filepath.Join(commonDir, "heavyq", "notify") }

// Outcome of one Check.
type Outcome struct {
	Message *Message // what was decided to push; nil when nothing
	Channel string   // the channel that delivered it; "" when none did
	Err     error    // delivery failed on every channel (the state is kept regardless)
}

// Check compares the facts with the last notified state, and when something is
// new pushes one coalesced message. The common case (nothing changed) is one
// small file read and no lock. The state is written before the delivery, so a
// crash or a failing channel never repeats a notice; and concurrent callers
// (every open status line asks) serialise on a lock, the loser skips.
func Check(commonDir string, f Facts, o Options) Outcome {
	if f.Now.Sub(f.Updated) > MaxAge {
		return Outcome{} // a stale state is a stopped materialiser, not news
	}
	dir := Dir(commonDir)
	prev := readState(dir)
	if next, msg := Evaluate(prev, f, o.Budget); msg == nil && sameState(prev, next) {
		return Outcome{}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Outcome{Err: err}
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return Outcome{Err: err}
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return Outcome{} // another status line is deciding right now
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	next, msg := Evaluate(readState(dir), f, o.Budget) // again, under the lock
	if err := writeState(dir, next); err != nil {
		return Outcome{Err: err}
	}
	if msg == nil {
		return Outcome{}
	}
	var chans []Channel
	if o.Channels != nil {
		chans = o.Channels()
	}
	name, err := Deliver(chans, *msg)
	return Outcome{Message: msg, Channel: name, Err: err}
}

// readState loads the dedupe record; nil when there is none or it is unreadable.
func readState(dir string) *State {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil
	}
	var s State
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// writeState replaces the record atomically.
func writeState(dir string, s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "state.json")); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// sameState: the record on disk already says what next would say.
func sameState(prev *State, next State) bool {
	if prev == nil {
		return false
	}
	a, b := *prev, next
	return a.Day == b.Day && a.Sent == b.Sent && a.Suppressed == b.Suppressed && a.AtRisk == b.AtRisk &&
		a.AllProven == b.AllProven && equalStrings(a.Needs, b.Needs)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
