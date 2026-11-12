package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// nextUntrusted is the one thing to do when a remote's exit codes are not trustworthy.
const nextUntrusted = "fix that remote's PATH (a setsid that is not util-linux's?), or use another remote or --remote local; see docs/remote.md"

// canaryDir is where exit canary verdicts are kept: beside the ledger ($XDG_STATE_HOME/heavyq/exit-canary, or
// beside $HEAVYQ_HISTORY). "" when there is no state directory: the canary then runs before every remote run.
func canaryDir() string {
	p := history.DefaultPath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "exit-canary")
}

// untrustedReason is why the stored canary verdict of m says its exit codes are lost, "" when it does not
// (trusted, never checked, or checked for another job script or setup). A file read: fine for hot paths.
func untrustedReason(m config.MachineConfig) string {
	if m.IsLocal() {
		return ""
	}
	if v, ok := remote.LoadCanary(canaryDir(), m); ok && !v.Trusted {
		return v.Reason
	}
	return ""
}

// untrustedNote is "exit codes not trustworthy on <m>: <why>" when untrustedReason says so, else "".
func untrustedNote(m config.MachineConfig) string {
	if why := untrustedReason(m); why != "" {
		return snapshot.UntrustedExitText(m.Name, why)
	}
	return ""
}

// proving reports whether the run's result is meant as proof: a gate, an item proof, a result to cache, or
// --strict. Such a run never goes to a remote whose exit codes are not trustworthy.
func (r *run) proving() bool {
	return r.opts.Gate != "" || r.opts.Item != "" || r.opts.Cache || r.opts.Strict
}

// checkExitTrust makes sure remote m returns exit codes before a run there: the stored canary verdict when
// it is fresh, else the canary now, through the job path the run takes (the run's commit, already pushed,
// and its slot). It returns 0 to go on, else heavyq's own ExitRemoteSetup (setupFailed: nothing ran). On an untrusted
// remote a proof is refused; any other run is refused too unless the remote sets allow_untrusted_exit, and
// then it runs with r.distrust set, so its record is suspect and never proof.
func (r *run) checkExitTrust(m config.MachineConfig, repo, slot string, timeout time.Duration) int {
	dir := canaryDir()
	v, ok := remote.LoadCanary(dir, m)
	if !ok || !v.Fresh(time.Now()) {
		trusted, reason, err := remote.RunCanary(m, remote.CanaryJob{Repo: repo, SHA: r.pl.sha, Lane: r.me.Lane, Slot: slot}, timeout)
		if err != nil {
			r.setupError(fmt.Errorf("the exit canary on %s could not run (%w); the command did not run", m.Name, err))
			return r.setupFailed()
		}
		v = remote.CanaryVerdict{Trusted: trusted, Reason: reason, At: time.Now()}
		if err := remote.SaveCanary(dir, m, v); err != nil {
			fmt.Fprintln(r.stderr, "heavyq: warning: exit canary verdict not stored:", err)
		}
	}
	if v.Trusted {
		return 0
	}
	r.distrust = snapshot.UntrustedExitText(m.Name, v.Reason)
	if r.proving() || !m.AllowUntrustedExit {
		// an agent reads these in the digest (digestRan), not as lines of their own
		r.note("%s; the command did not run (a result from there would prove nothing): %s", r.distrust, nextUntrusted)
		return r.setupFailed()
	}
	r.note("%s; running anyway (allow_untrusted_exit), but the result proves nothing", r.distrust)
	return 0
}
