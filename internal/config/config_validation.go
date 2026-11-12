package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// minPoll is the shortest queue.poll: every waiting ticket runs a scheduling
// round (queue flock, prune, list, local metrics) per poll, so a typo such as
// "2ms" for "2s" would turn each waiter into a busy loop on the machine
// heavyq protects.
const minPoll = 50 * time.Millisecond

// setDur fills the default of an absent or zero duration. A negative one is
// a typo, not "use the default", so it is refused with its key.
func setDur(d *Duration, def time.Duration, key string) error {
	if d.Duration < 0 {
		return fmt.Errorf("%s %v must not be negative (0 or absent = %v)", key, d.Duration, def)
	}
	if d.Duration == 0 {
		d.Duration = def
	}
	return nil
}

// normalize fills the [queue] defaults and refuses negative or busy-looping values.
func (q *QueueConfig) normalize() error {
	for _, d := range []struct {
		v   *Duration
		def time.Duration
		key string
	}{
		{&q.Poll, defaultPoll, "queue.poll"},
		{&q.ForceAfter, defaultForceAfter, "queue.force_after"},
		{&q.RemoteDownMax, defaultRemoteDownMax, "queue.remote_down_max"},
		{&q.HoldFor, defaultHoldFor, "queue.hold_for"},
		{&q.ProbeTimeout, defaultProbeTimeout, "queue.probe_timeout"},
	} {
		if err := setDur(d.v, d.def, d.key); err != nil {
			return err
		}
	}
	if q.Poll.Duration < minPoll {
		return fmt.Errorf("queue.poll %v must be at least %v", q.Poll.Duration, minPoll)
	}
	if q.LoadWeightMB < 0 {
		return fmt.Errorf("queue.load_weight_mb %d must not be negative (0 or absent = %d)", q.LoadWeightMB, defaultLoadWeightMB)
	}
	if q.LoadWeightMB == 0 {
		q.LoadWeightMB = defaultLoadWeightMB
	}
	if q.BaseRef != "" && q.BaseRef != BaseRefNone && !validBaseRef(q.BaseRef) {
		return fmt.Errorf("queue.base_ref %q must look like remote/branch (e.g. origin/main) or %q", q.BaseRef, BaseRefNone)
	}
	return nil
}

var (
	// BaseRefPattern is the shape of a --base-ref ("remote/branch"); ".." is refused on top of it.
	BaseRefPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$`)
	// SSHAliasPattern is a remote's ssh value: a plain host alias, options belong in ~/.ssh/config.
	SSHAliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)
)

func validBaseRef(s string) bool {
	return BaseRefPattern.MatchString(s) && !strings.Contains(s, "..")
}

// BaseRefNone is the value of --base-ref and queue.base_ref that means "no base ref": it also switches off
// the default (origin's default branch, when the repository has one).
const BaseRefNone = "none"

// ValidBaseRef reports whether s is a remote/branch ref, the shape --base-ref and queue.base_ref take
// (BaseRefNone is accepted by both as well, but is not a ref).
func ValidBaseRef(s string) bool { return validBaseRef(s) }
