package cli

import (
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// The rules of a move of a waiting ticket to other machines, applied by
// `heavyq retarget`, by the dashboard's POST tickets/{id}/target, and again by
// the ticket's own process when it reads the request (run_retarget.go). A move
// fails closed: what is not known to be safe is refused, and the ticket keeps
// its machines.

// checkRetarget refuses a move of ticket t to target where that is unsafe,
// whoever asks: a ticket that is running, a target that is not local, auto, a
// configured remote or a list of those, and a target that may leave this
// machine for a ticket that needs this machine's pool values or recorded no
// commit for a remote to run (it was queued to run here only).
func checkRetarget(cfg *config.Config, t *queue.Ticket, where string) error {
	if t.Running {
		return fmt.Errorf("lane %s (pid %d) is already running on %s; only a waiting ticket moves", t.Lane, t.Pid, t.Machine)
	}
	if err := cfg.CheckRemoteTarget(where); err != nil {
		return fmt.Errorf("--remote: %w (config %s)", err, cfgName(cfg))
	}
	if where == config.LocalName {
		return nil
	}
	if len(t.Needs) > 0 {
		return fmt.Errorf("lane %s (pid %d) asks for pool values of this machine (--needs %s), so it runs here only (--remote local)",
			t.Lane, t.Pid, strings.Join(t.Needs, ","))
	}
	if t.Commit == "" {
		return fmt.Errorf("lane %s (pid %d) was queued to run on this machine only, so it recorded no commit a remote could run; "+
			"to move it, cancel it (heavyq cancel %s) and queue it again with --remote %s inside a git repository "+
			"(a remote runs a commit, never uncommitted edits)", t.Lane, t.Pid, t.Name, where)
	}
	return nil
}

// vetRetarget is checkRetarget plus pause, which only the requester checks: a
// paused machine admits nothing new (`heavyq pause`), so a move onto paused
// machines only would just park the ticket, and is refused. The paused machines
// of a target that has others are returned: the ticket skips them until they
// are resumed, as every ticket does.
func vetRetarget(cfg *config.Config, t *queue.Ticket, where string) ([]string, error) {
	if err := checkRetarget(cfg, t, where); err != nil {
		return nil, err
	}
	ms := (&placement{target: where, remotes: remoteNames(cfg)}).machines()
	var paused []string
	for _, m := range ms {
		if controlBox().Paused(m) {
			paused = append(paused, m)
		}
	}
	if len(ms) > 0 && len(paused) == len(ms) {
		verb := "is"
		if len(paused) > 1 {
			verb = "are all"
		}
		return nil, fmt.Errorf("%s %s paused: a paused machine admits nothing new, so the ticket would only wait there "+
			"(heavyq resume %s, or pick another machine)", strings.Join(paused, ", "), verb, paused[0])
	}
	return paused, nil
}

// retargetDetail says what the moved ticket ships to a remote and which
// machines of its new target it skips while they are paused.
func retargetDetail(t *queue.Ticket, where string, paused []string) string {
	var parts []string
	if where != config.LocalName && t.Commit != "" {
		parts = append(parts, fmt.Sprintf("a remote runs %s@%.12s", t.Repo, t.Commit))
	}
	for _, m := range paused {
		parts = append(parts, fmt.Sprintf("%s is paused: skipped until heavyq resume %s", m, m))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}
