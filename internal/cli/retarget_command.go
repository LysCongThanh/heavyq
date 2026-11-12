package cli

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

const retargetUsage = "usage: heavyq retarget <ticket|pid> --remote WHERE   (WHERE: local, auto, a remote name or a list a,b)"

// retargetAnswerPoll is how often `heavyq retarget` looks for the ticket's answer.
const retargetAnswerPoll = 50 * time.Millisecond

// retargetCommand implements `heavyq retarget <ticket|pid> --remote WHERE`: a
// waiting ticket waits for the machines of WHERE from its next poll on, as if
// it had been queued with --remote WHERE, and keeps its priority and its place.
// The move is checked here (retarget_rules.go), filed in the control mailbox,
// and checked again by the ticket's own process, whose answer decides the exit
// code: 0 when it moved (or has not read the request within a few polls: it
// moves when it does), 2 when the move is refused and nothing changed.
func retargetCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	ref, where, ok := parseRetargetArgs(args, stderr)
	if !ok {
		return ExitUsage
	}
	t, err := findTicket(ref)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	paused, err := vetRetarget(cfg, t, where)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	if where == t.Target {
		fmt.Fprintf(stdout, "lane %s (pid %d) already waits for %s\n", t.Lane, t.Pid, where)
		return 0
	}
	awaitTicketConfig(cfg, t)
	if err := controlBox().RequestTarget(t.Name, where); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return awaitRetarget(cfg, t, where, paused, stdout, stderr)
}

// parseRetargetArgs reads `<ticket|pid> --remote WHERE`; the flag may come before,
// after or as --remote=WHERE. It says what is wrong on stderr and returns false
// unless exactly one ticket and a non-empty target were given.
func parseRetargetArgs(args []string, stderr io.Writer) (ref, where string, ok bool) {
	fs := flag.NewFlagSet("retarget", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, retargetUsage) }
	remoteFlag := fs.String("remote", "", "local | auto | <remote name> | a,b: the machines the ticket may run on")
	var pos []string
	for { // the ticket and the flag may come in any order
		if err := fs.Parse(args); err != nil {
			return "", "", false
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	if len(pos) != 1 || *remoteFlag == "" {
		fs.Usage()
		return "", "", false
	}
	return pos[0], *remoteFlag, true
}

// awaitRetarget waits a few polls for the answer of the ticket's own process to
// the request just filed. The ticket's state is read before the answer, so an
// answer written right before the ticket got its slot is never missed.
func awaitRetarget(cfg *config.Config, t *queue.Ticket, where string, paused []string, stdout, stderr io.Writer) int {
	box := controlBox()
	who := fmt.Sprintf("lane %s (pid %d)", t.Lane, t.Pid)
	deadline := time.Now().Add(2*cfg.Queue.Poll.Duration + cfg.Queue.ProbeTimeout.Duration + time.Second)
	for {
		left := leftQueue(t)
		if why, ok := box.TargetAnswer(t.Name); ok {
			if why != "" {
				fmt.Fprintf(stderr, "heavyq: %s kept its machines (%s): %s\n", who, t.Target, why)
				return ExitUsage
			}
			fmt.Fprintf(stdout, "%s now waits for %s%s\n", who, where, retargetDetail(t, where, paused))
			return 0
		}
		if left != "" {
			fmt.Fprintf(stderr, "heavyq: %s %s before it read the move; nothing changed\n", who, left)
			return ExitUsage
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stdout, "%s moves to %s on its next poll%s\n", who, where, retargetDetail(t, where, paused))
			return 0
		}
		time.Sleep(retargetAnswerPoll)
	}
}

// leftQueue says how ticket t stopped waiting ("" while it still waits).
func leftQueue(t *queue.Ticket) string {
	q, err := queue.Open(QueueDir())
	if err != nil {
		return "" // cannot tell: keep waiting for the answer until the deadline
	}
	now, ok := q.Find(t.Name)
	switch {
	case !ok:
		return "left the queue"
	case now.Running:
		return "started on " + now.Machine
	}
	return ""
}
