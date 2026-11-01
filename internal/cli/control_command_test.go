package cli

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// A pid shared by several tickets (an older heavyq ran every command of
// `gate run --parallel` in one process) must not be guessed at: a cancel
// cannot be undone. A ticket id always works.
func TestFindTicketRefusesAnAmbiguousPid(t *testing.T) {
	t.Setenv("HEAVYQ_DIR", t.TempDir())
	q, err := queue.Open(QueueDir())
	if err != nil {
		t.Fatal(err)
	}
	start, err := procinfo.Default.StartTime(os.Getpid())
	if err != nil {
		t.Skip("cannot read own start time:", err)
	}
	var tickets []*queue.Ticket
	for _, lane := range []string{"one", "two"} {
		tk := &queue.Ticket{Pid: os.Getpid(), Start: start, Lane: lane, Prio: 3, Target: "local",
			Candidates: []string{"local"}, Since: time.Now(), Cmd: "x"}
		if err := q.Add(tk); err != nil {
			t.Fatal(err)
		}
		tickets = append(tickets, tk)
	}
	pid := strconv.Itoa(os.Getpid())
	_, err = findTicket(pid)
	if err == nil || !strings.Contains(err.Error(), "2 tickets") || !strings.Contains(err.Error(), tickets[0].Name) {
		t.Fatalf("an ambiguous pid must be refused with the ticket ids, got %v", err)
	}
	got, err := findTicket(tickets[1].Name)
	if err != nil || got.Lane != "two" {
		t.Fatalf("a ticket id is never ambiguous: %v %v", got, err)
	}
	q.Remove(tickets[1])
	if got, err := findTicket(pid); err != nil || got.Lane != "one" {
		t.Fatalf("one ticket left: pid is unambiguous again: %v %v", got, err)
	}
}
