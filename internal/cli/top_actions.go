package cli

import (
	"errors"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/control"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// topActions are the control requests of `heavyq top --control`: the same
// mailbox, with the same checks, as the dashboard's POST handlers
// (internal/dash/dash_api_handlers.go). Nothing here signals a process: a
// request is a file the ticket's own heavyq picks up on its next poll.
type topActions struct {
	cfg *config.Config
	q   *queue.Dir
	box control.Mailbox
}

func newTopActions(cfg *config.Config) (*topActions, error) {
	q, err := queue.Open(QueueDir())
	if err != nil {
		return nil, err
	}
	return &topActions{cfg: cfg, q: q, box: controlBox()}, nil
}

var errGone = errors.New("no such ticket (it may have finished)")

// Cancel asks the owner of ticket to stop it (waiting: leave the queue;
// running: SIGTERM to its process tree).
func (a *topActions) Cancel(ticket string) error {
	if !control.ValidTicket(ticket) {
		return control.ErrBadName
	}
	if _, ok := a.q.Find(ticket); !ok {
		return errGone
	}
	return a.box.RequestCancel(ticket)
}

// Prio moves a waiting ticket to priority prio (1-9).
func (a *topActions) Prio(ticket string, prio int) error {
	if !control.ValidTicket(ticket) {
		return control.ErrBadName
	}
	t, ok := a.q.Find(ticket)
	if !ok {
		return errGone
	}
	if t.Running {
		return errors.New("the ticket is already running; only waiting tickets change priority")
	}
	return a.box.RequestPrio(ticket, prio)
}

// SetPaused pauses or resumes admissions on a configured machine.
func (a *topActions) SetPaused(machine string, paused bool) error {
	if _, ok := a.cfg.Machine(machine); !ok {
		return errors.New("no such machine in the configuration")
	}
	return a.box.SetPaused(machine, paused)
}

// topWhy is `heavyq why <lane>` for the Enter key: the lane's tickets, the
// reason per machine, and its last run from the ledger index (never a scan).
// A broken history only loses the last-run line.
func topWhy(lane string, s snapshot.Snapshot, now time.Time) []string {
	last, _, err := ledgerindex.LastOfLane(history.DefaultPath(), queue.SanitizeLane(lane))
	if err != nil {
		last = nil
	}
	return snapshot.LaneLines(s, queue.SanitizeLane(lane), last, now)
}
