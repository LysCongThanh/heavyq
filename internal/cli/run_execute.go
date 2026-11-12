package cli

import (
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/runident"
)

// execute runs the command on the claimed machine and records history.
func (r *run) execute(c *claim) int {
	waited := time.Since(r.t0)
	co := r.checkoutFor(c.machine)      // the code the command is about to see
	whole := r.wholeTree(c.machine, co) // the same code as the cache key sees it (the whole tree, for a gate with paths)
	r.announceAdmitted(c, waited, co)
	if !r.out.mode.Terse() { // an agent reads it in the digest (digestRan), not as a line of its own
		warnShellPipe(r.stderr, r.opts.Argv)
	}
	runStart := time.Now()
	var rc int
	if c.machine == config.LocalName {
		r.base, r.baseFixed = runident.BaseSHA(r.localDir(), r.opts.BaseRef), true // what the command sees now
		rc = r.runLocal(c)
		co = r.settled(co)
		if r.wholeTree(c.machine, co) != whole { // an edit outside a gate's paths leaves co alone, not the result
			whole = ""
		}
	} else {
		rc = r.runRemote(c)
	}
	runSecs := time.Since(runStart).Seconds()
	rc = r.judgeSuspect(rc)
	ran := !r.interrupted && !r.cancelled && !r.remoteSetupFailed && rc != 126 && rc != 127 &&
		(c.machine == config.LocalName || rc != ExitConnLost) && r.suspect == ""
	r.cacheFinish(c.machine, rc, ran, runSecs, whole)
	rec := history.Record{End: time.Now(), Machine: c.machine, Lane: r.me.Lane, Prio: r.me.Prio, Light: r.me.Light,
		WaitSecs: waited.Seconds(), RunSecs: runSecs, Exit: rc, Cmd: r.me.Cmd, Forced: c.forced,
		CPUSecs: r.cpuSecs, MaxRSSMB: r.maxRSSMB}
	r.tag(&rec, co)
	c.stampAdmission(&rec)
	switch {
	case r.cancelled:
		rec.Result = history.OutcomeCancelled
	case r.interrupted:
		rec.Result = history.OutcomeInterrupted
	case r.suspect != "":
		rec.Result = history.OutcomeSuspect // exit 0 (127 under --strict), but the shell lost a command: no proof
	case r.remoteSetupFailed || rc == 126 || rc == 127:
		rec.Result = history.OutcomeGaveUp // the command never started (a command's own 77 is its failure)
	case rc == ExitConnLost && c.machine != config.LocalName:
		// The connection dropped and the remote stopped the command: it did not
		// run to its end, so it proves nothing either way (remote_job.sh remaps
		// a command's own 255, so this 255 is always the transport's).
		rec.Result = history.OutcomeInterrupted
	case r.distrust != "":
		rec.Result = history.OutcomeSuspect // the remote loses exit codes (allow_untrusted_exit): no proof
	}
	if r.cache != nil {
		rec.Cache, rec.CacheNote = r.cache.status, r.cache.note
	}
	if err := ledgerindex.Append(history.DefaultPath(), rec); err != nil {
		fmt.Fprintln(r.stderr, "heavyq: warning: history not written:", err)
	}
	r.digestRan(rc, c.machine, co, time.Since(runStart), waited)
	return rc
}

// localDir is where a local command runs: the caller's cwd, unless --repo
// points elsewhere and the cwd is outside it (see runDir).
func (r *run) localDir() string { return runDir(r.pl.wd, r.opts.Repo) }
