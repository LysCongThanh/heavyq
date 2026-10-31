package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// run is the state of one `heavyq <prio> -- cmd` invocation.
type run struct {
	cfg    *config.Config
	opts   runOptions
	pl     *placement
	q      *queue.Dir
	me     *queue.Ticket
	probes *probeCache
	sigs   chan os.Signal
	stderr io.Writer
	t0     time.Time
}

func runCommand(cfg *config.Config, args []string, stderr io.Writer) int {
	o, err := parseRunArgs(args, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	pl, err := preparePlacement(cfg, &o, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	q, err := queue.Open(QueueDir())
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	start, err := procinfo.Default.StartTime(os.Getpid())
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: cannot read own process start time:", err)
		return ExitUsage
	}
	r := &run{cfg: cfg, opts: o, pl: pl, q: q, stderr: stderr, t0: time.Now(),
		probes: newProbeCache(cfg.Queue.ProbeTimeout.Duration),
		sigs:   make(chan os.Signal, 4)}
	signal.Notify(r.sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(r.sigs)

	// Probe remotes once before enqueuing, outside the mutex, so a hanging
	// ssh never delays anyone else's scheduling round.
	r.probes.refresh(cfg, pl.candidates)
	r.me = &queue.Ticket{
		Pid: os.Getpid(), Start: start, Lane: o.Lane, Prio: o.Prio, Light: o.Light,
		Target: pl.target, Candidates: pl.candidates, Since: r.t0, Cmd: queue.TruncateCmd(o.Argv),
	}
	if err := q.Add(r.me); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	defer q.Remove(r.me)

	c, rc := r.waitForTurn()
	if c == nil {
		return rc
	}
	defer c.lock.Release()
	return r.execute(c)
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
