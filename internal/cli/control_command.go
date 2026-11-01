package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// controlCommand implements `heavyq cancel|reprio|pause|resume`.
//
//	heavyq cancel <ticket|pid>
//	heavyq reprio <ticket|pid> <1-9>
//	heavyq pause  <machine>
//	heavyq resume <machine>
func controlCommand(cfg *config.Config, verb string, args []string, w, stderr io.Writer) int {
	box := controlBox()
	switch verb {
	case "pause", "resume":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "usage: heavyq %s <machine>\n", verb)
			return ExitUsage
		}
		if _, ok := cfg.Machine(args[0]); !ok {
			fmt.Fprintf(stderr, "heavyq: no machine %q in the configuration\n", args[0])
			return ExitUsage
		}
		if err := box.SetPaused(args[0], verb == "pause"); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		fmt.Fprintf(w, "%s: %sd (running jobs are not affected)\n", args[0], verb)
		return 0
	}
	want := 1
	if verb == "reprio" {
		want = 2
	}
	if len(args) != want {
		fmt.Fprintln(stderr, "usage: heavyq cancel <ticket|pid>  |  heavyq reprio <ticket|pid> <1-9>")
		return ExitUsage
	}
	t, err := findTicket(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	if verb == "cancel" {
		if err := box.RequestCancel(t.Name); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		fmt.Fprintf(w, "cancel requested for lane %s (pid %d); it stops within a few seconds\n", t.Lane, t.Pid)
		return 0
	}
	p, err := strconv.Atoi(args[1])
	if err == nil && t.Running {
		err = fmt.Errorf("lane %s (pid %d) is already running", t.Lane, t.Pid)
	}
	if err == nil {
		err = box.RequestPrio(t.Name, p)
	}
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	fmt.Fprintf(w, "lane %s (pid %d) moves to priority %d on its next poll\n", t.Lane, t.Pid, p)
	return 0
}

// findTicket resolves a ticket name or the pid of its heavyq process.
func findTicket(ref string) (*queue.Ticket, error) {
	q, err := queue.Open(QueueDir())
	if err != nil {
		return nil, err
	}
	if t, ok := q.Find(ref); ok {
		return t, nil
	}
	pid, err := strconv.Atoi(ref)
	if err != nil {
		return nil, fmt.Errorf("no ticket %q (use the pid shown by heavyq status)", ref)
	}
	s, err := q.List()
	if err != nil {
		return nil, err
	}
	var same []*queue.Ticket
	for _, t := range append(s.Waiting, s.Running...) {
		if t.Pid == pid {
			same = append(same, t)
		}
	}
	switch len(same) {
	case 0:
		return nil, fmt.Errorf("no ticket with pid %d", pid)
	case 1:
		return same[0], nil
	}
	// Never guess which of several tickets was meant: a cancel is not undoable.
	var ids []string
	for _, t := range same {
		ids = append(ids, t.Name)
	}
	return nil, fmt.Errorf("pid %d has %d tickets (%s); name one by its ticket id", pid, len(same), strings.Join(ids, ", "))
}
