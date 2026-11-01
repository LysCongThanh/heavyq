package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// followerPoll is how often a run waiting on an identical in-flight run
// checks whether that run finished.
const followerPoll = 200 * time.Millisecond

// cacheState is the --cache bookkeeping of one run.
type cacheState struct {
	store  *cache.Store
	flight *cache.Flight
	key    string
	tree   string          // the clean tree id the key was computed on
	lead   *slots.Lock     // held while we run the command for everyone
	rec    *cache.Recorder // nil: output not recorded
	status string          // history "cache" field
	note   string          // why the result was not used or stored
}

func newStore(r *run) *cache.Store {
	c := r.cfg.Cache
	return &cache.Store{Dir: cache.DefaultDir(), TTL: c.TTL.Duration,
		MaxBytes: int64(c.MaxSizeMB) << 20, MaxEntryBytes: int64(c.MaxEntryMB) << 20}
}

// cachePhase runs before the ticket is enqueued. done reports that the run
// was answered from a stored or coalesced result, with exit code rc.
func (r *run) cachePhase(lane string) (rc int, done bool) {
	if !r.opts.Cache {
		return 0, false
	}
	cs := &cacheState{store: newStore(r), flight: &cache.Flight{Dir: filepath.Join(QueueDir(), "inflight")}, status: "skip"}
	r.cache = cs
	skip := func(why string) (int, bool) {
		cs.note = why
		r.verbosef("cache not used: %s", why)
		return 0, false
	}
	switch {
	case cs.store.Dir == "":
		return skip("no cache directory (set XDG_CACHE_HOME or HOME)")
	case r.opts.Ref != "HEAD":
		return skip("--ref other than HEAD")
	case len(r.opts.With) > 0:
		return skip("--with sibling repos are not part of the key")
	}
	key, tree, why := cache.KeyAndTree(cache.KeyInput{Dir: r.localDir(), Argv: r.opts.Argv, EnvNames: r.cfg.Cache.Env,
		Version: version(), BaseRef: r.opts.BaseRef, Getenv: os.LookupEnv})
	if why != "" {
		return skip(why)
	}
	cs.key, cs.tree = key, tree
	r.verbosef("cache key %.16s", key)
	me := cache.Party{Key: key, Pid: os.Getpid(), Start: r.start, Lane: lane, Cmd: queue.TruncateCmd(r.opts.Argv), Since: r.t0}
	waited := false
	unfollow := func() {}
	defer func() { unfollow() }()
	for {
		m := cs.store.Get(key, time.Now())
		if m != nil && (m.Exit == 0 || r.cfg.Cache.CacheFailures || (waited && !m.Created.Before(r.t0))) {
			if rc, ok := r.replayCached(m, waited, lane); ok {
				return rc, true
			}
		}
		l, err := cs.flight.TryLead(me)
		if err != nil {
			return skip("in-flight lock: " + err.Error())
		}
		if l != nil {
			cs.lead, cs.status = l, "miss"
			if waited {
				r.verbosef("the identical run ended without a reusable result; running it ourselves")
			}
			if cs.rec, err = cs.store.NewRecorder(os.Stdout, os.Stderr); err != nil {
				cs.note = "not recorded: " + err.Error()
			}
			return 0, false
		}
		if !waited {
			waited = true
			leader, _ := cs.flight.Leader(key)
			fmt.Fprintf(r.stderr, "heavyq: an identical run is in progress (lane %s); waiting for its result instead of running it again\n", leader.Lane)
			if unfollow, err = cs.flight.Follow(me); err != nil {
				r.verbosef("cannot register as waiting: %v", err)
			}
		}
		if r.opts.MaxWait > 0 && time.Since(r.t0) >= r.opts.MaxWait {
			fmt.Fprintf(r.stderr, "heavyq: the identical run did not finish within %s (--max-wait)\n", fmtDur(r.opts.MaxWait))
			return ExitMaxWait, true
		}
		if s := r.pollOrSignal(followerPoll); s != nil {
			return signalExit(s), true
		}
	}
}

// replayCached prints a stored result. ok is false when the entry vanished
// before its output could be opened (treated as a miss).
func (r *run) replayCached(m *cache.Meta, coalesced bool, lane string) (int, bool) {
	opened, err := r.cache.store.Replay(m, os.Stdout, os.Stderr)
	if !opened {
		return 0, false
	}
	if err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: cached output incomplete:", err)
	}
	kind, when := "hit", m.Created.Local().Format("2006-01-02 15:04:05")
	if coalesced {
		kind = "coalesced"
		fmt.Fprintf(r.stderr, "heavyq: reused the result of an identical run (lane %s) from %s (%s)\n", m.Lane, when, m.Machine)
	} else {
		fmt.Fprintf(r.stderr, "heavyq: cached result from %s (%s)\n", when, m.Machine)
	}
	r.cache.status = kind
	rec := history.Record{End: time.Now(), Machine: m.Machine, Lane: lane, Prio: r.opts.Prio, Light: r.opts.Light,
		WaitSecs: time.Since(r.t0).Seconds(), Exit: m.Exit, Cmd: queue.TruncateCmd(r.opts.Argv), Cache: kind,
		CacheNote: fmt.Sprintf("key %.16s, from %s", m.Key, when)}
	r.tag(&rec, r.replayedCheckout())
	if err := history.Append(history.DefaultPath(), rec); err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: history not written:", err)
	}
	return m.Exit, true
}

// replayedCheckout is the identity to record for a result that was replayed
// instead of run. The result proves the tree the cache key was computed on, not
// whatever the work tree holds now: a follower can wait minutes for the leader,
// and a plain hit still takes time between key and replay. So the tree is the
// key's when the work tree still has it, and unknown ("") when it changed.
func (r *run) replayedCheckout() gitutil.Checkout {
	co := r.checkoutFor(config.LocalName)
	if co.Tree != r.cache.tree {
		r.verbosef("ledger: the work tree changed since the cache key was computed; no tree identity recorded")
		co.Tree = ""
	}
	return co
}

// cacheFinish stores the leader's result (when the command really ran on
// the same clean tree) and hands leadership back.
func (r *run) cacheFinish(machine string, rc int, ran bool, runSecs float64) {
	cs := r.cache
	if cs == nil || cs.lead == nil || cs.rec == nil {
		return
	}
	rec := cs.rec
	cs.rec = nil
	key, why := cache.Key(cache.KeyInput{Dir: r.localDir(), Argv: r.opts.Argv, EnvNames: r.cfg.Cache.Env,
		Version: version(), BaseRef: r.opts.BaseRef, Getenv: os.LookupEnv})
	switch {
	case !ran:
		rec.Abort()
		cs.note = "the command did not run to completion; nothing stored"
	case key != cs.key:
		rec.Abort()
		cs.note = "the work tree changed during the run (" + orDefault(why, "different commit") + "); nothing stored"
	default:
		err := cs.store.Commit(rec, cache.Meta{Key: cs.key, Created: time.Now(), Exit: rc, Machine: machine,
			Lane: r.me.Lane, Cmd: r.me.Cmd, RunSecs: runSecs})
		switch {
		case err != nil:
			cs.note = "not stored: " + err.Error()
		case rc != 0 && !r.cfg.Cache.CacheFailures:
			cs.note = fmt.Sprintf("exit %d is shared with runs that waited, but not cached (cache_failures = false)", rc)
		default:
			cs.note = "stored"
		}
	}
	r.verbosef("cache: %s", cs.note)
}

// cacheRelease ends leadership, whatever happened (deferred by runCommand).
func (r *run) cacheRelease() {
	if r.cache == nil || r.cache.lead == nil {
		return
	}
	if r.cache.rec != nil {
		r.cache.rec.Abort()
	}
	r.cache.flight.EndLead(r.cache.key, r.cache.lead)
	r.cache.lead = nil
}

// cmdStreams are the stdout/stderr given to the command.
func (r *run) cmdStreams() (io.Writer, io.Writer) {
	if r.cache != nil && r.cache.rec != nil {
		return r.cache.rec.Stdout(), r.cache.rec.Stderr()
	}
	return os.Stdout, os.Stderr
}

func (r *run) verbosef(format string, a ...any) {
	if r.opts.Verbose {
		fmt.Fprintf(r.stderr, "heavyq: "+format+"\n", a...)
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
