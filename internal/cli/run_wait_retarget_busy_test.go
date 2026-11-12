package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// A ticket moved by heavyq retarget restarts its anti-starvation clock even
// when the round right after the move skips on a busy queue mutex: waitForTurn
// clears moved after that round, so a reset left behind the mutex would let
// the old machines' clock carry over to the new ones.
func TestRetargetRestartsClockWhenRoundSkipsOnBusyMutex(t *testing.T) {
	q, err := queue.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := q.Mutex(time.Second, nil) // another heavyq holds the mutex
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	r := &run{q: q, me: &queue.Ticket{Name: "t-busy"}, moved: true}
	r.stopAsked.Store(true)                 // give up on the busy mutex at once
	headSince := time.Now().Add(-time.Hour) // the clock on the old machines
	if _, _, _, _, err := r.round(&headSince); !errors.Is(err, queue.ErrMutexBusy) {
		t.Fatalf("round behind a held mutex: err=%v, want ErrMutexBusy", err)
	}
	if !headSince.IsZero() {
		t.Fatalf("the anti-starvation clock survived the move: %v", headSince)
	}
}
