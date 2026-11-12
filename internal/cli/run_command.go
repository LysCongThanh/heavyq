package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// run is the state of one `heavyq <prio> -- cmd` invocation.
type run struct {
	cfg *config.Config
	// cfgWatch re-reads the config file while the ticket waits (run_config_reload.go);
	// poolRefusal is the last reason an edit was refused for its pools, said once.
	cfgWatch    *config.Watch
	poolRefusal string
	opts        runOptions
	pl          *placement
	q           *queue.Dir
	me          *queue.Ticket
	probes      *probeCache
	sigs        chan os.Signal
	stderr      io.Writer
	t0          time.Time
	start       string      // our process start token
	cache       *cacheState // nil without --cache
	// interrupted: a signal stopped the command, so its result is not real.
	interrupted bool
	// cancelled: an operator stopped the ticket (heavyq cancel / dashboard).
	cancelled bool
	// tree is the local command's process tree (nil for remote runs); an
	// operator cancel signals all of it, not just the direct child.
	tree *processTree
	// usage of the local command once it exited (zero for remote runs).
	cpuSecs  float64
	maxRSSMB int
	// out words the run's own output and keeps what the command printed (agent mode, --digest).
	out runOutput
	// poolSet is the pools the run's needs refer to (nil without --needs).
	poolSet *pools.Set
	// scan watches the command's output for a shell not-found line; suspect is
	// that line when the command nevertheless exited 0 (see run_suspect_judge.go).
	scan    *suspectScan
	suspect string
}

func runCommand(cfg *config.Config, args []string, stderr io.Writer) int {
	mode := outputMode(stderr)
	o, err := parseRunArgs(args, cfg)
	if err != nil {
		return usageFail(stderr, mode, err)
	}
	pl, err := preparePlacement(cfg, &o, stderr)
	if err != nil {
		return usageFail(stderr, mode, err)
	}
	q, err := queue.Open(QueueDir())
	if err != nil {
		return usageFail(stderr, mode, err)
	}

	defer renewLaneLeases(o.Lane) // a run in a lane keeps that lane's item claims alive (fail-open)
	// The end of a run is a natural event: flow/state.json is brought up to date (debounced, fail-open).
	defer refreshFlowAfterRun(cfg)

	if o.Session == "" { // explicit flag and $HEAVYQ_SESSION win over the hook's file
		o.Session = q.ReadSession(o.Lane, time.Now(), history.ValidTag)
	}
	start, err := procinfo.Default.StartTime(os.Getpid())
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: cannot read own process start time:", err)
		return ExitUsage
	}
	r := &run{cfg: cfg, opts: o, pl: pl, q: q, stderr: stderr, t0: time.Now(), start: start,
		probes: newProbeCache(cfg.Queue.ProbeTimeout.Duration), cfgWatch: config.NewWatch(cfg),
		sigs: make(chan os.Signal, 4), scan: &suspectScan{}}
	r.out = newRunOutput(mode, o.Digest, stderr, r.t0, o.Lane)
	defer r.out.discardLog()
	signal.Notify(r.sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(r.sigs)
	defer r.cacheRelease()
	if rc, done := r.cachePhase(o.Lane); done {
		return rc
	}

	// Probe remotes once before enqueuing, outside the mutex, so a hanging
	// ssh never delays anyone else's scheduling round.
	r.probes.refresh(cfg, pl.candidates)
	r.me = &queue.Ticket{
		Pid: os.Getpid(), Start: start, Lane: o.Lane, Prio: o.Prio, Light: o.Light,
		Target: pl.target, Candidates: pl.candidates, Since: r.t0, Cmd: queue.TruncateCmd(o.Argv), With: siblingLabels(pl.siblings),
		ConfigSum: cfg.Origin.Sum,
	}
	if err := r.preparePools(); err != nil {
		return usageFail(stderr, mode, err)
	}
	if err := q.Add(r.me); err != nil {
		return usageFail(stderr, mode, err)
	}
	defer q.Remove(r.me)
	defer func() { controlBox().Forget(r.me.Name) }()

	c, rc := r.waitForTurn()
	if c == nil {
		r.recordNoRun(rc)
		r.digestNotRun(rc)
		return rc
	}
	defer c.releaseAll()
	return r.execute(c)
}

// recordNoRun writes a history line for a ticket that left the queue
// without running: gave up (max-wait, remote down), cancelled or interrupted.
// Usage errors are not recorded.
func (r *run) recordNoRun(rc int) {
	result := history.OutcomeGaveUp
	switch {
	case r.cancelled:
		result = history.OutcomeCancelled
	case rc > 128 && rc != ExitConnLost:
		result = history.OutcomeInterrupted
	case rc != ExitMaxWait && rc != ExitRemoteDown:
		return
	}
	rec := history.Record{End: time.Now(), Lane: r.me.Lane, Prio: r.me.Prio, Light: r.me.Light,
		WaitSecs: time.Since(r.t0).Seconds(), Exit: rc, Cmd: r.me.Cmd, Result: result}
	r.tag(&rec, r.checkoutFor(""))
	if err := ledgerindex.Append(history.DefaultPath(), rec); err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: history not written:", err)
	}
}

// signalExit maps a signal to the conventional 128+n exit code.
func signalExit(s os.Signal) int {
	if n, ok := s.(syscall.Signal); ok {
		return 128 + int(n)
	}
	return 130
}

// remoteCandidates lists the non-local machines among names.
func remoteCandidates(names []string) []string {
	var out []string
	for _, n := range names {
		if n != config.LocalName {
			out = append(out, n)
		}
	}
	return out
}

// pollOrSignal sleeps for d unless a signal arrives (returned).
func (r *run) pollOrSignal(d time.Duration) os.Signal {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case s := <-r.sigs:
		return s
	case <-t.C:
		return nil
	}
}

func fmtDur(d time.Duration) string { return d.Round(time.Second).String() }
