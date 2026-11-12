package control

import (
	"fmt"
	"os"
	"testing"
)

// targetWriterEnv turns the helper writer (control_mailbox_concurrency_test.go)
// into one that files target requests, as `heavyq retarget` and the dashboard do.
const targetWriterEnv = "HEAVYQ_CONTROL_TEST_TARGET_WRITER"

// targetBurst files n-1 target requests for "box", then "final" as its last word.
func targetBurst(m Mailbox, n int) string {
	for i := 0; i < n; i++ {
		target := "box"
		if i == n-1 {
			target = "final"
		}
		if err := m.RequestTarget(tk, target); err != nil {
			return fmt.Sprint("ERR ", err)
		}
	}
	return "OK"
}

// The owner polls TakeTarget while an operator keeps moving the ticket. The
// operator's last move must reach the owner: consumed by a poll, or still
// pending for the next one. Never deleted unseen, which would leave the
// operator reading the answer to an older move as the answer to this one.
func TestTakeTargetNeverDropsTheLatestRequest(t *testing.T) {
	m := Mailbox{Dir: t.TempDir() + "/ctl"}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w := startWriter(t, m.Dir, 20, 0, targetWriterEnv+"=1")
	for round := 0; round < 200; round++ {
		done := make(chan struct{})
		seen := make(chan string, 1)
		go func() { // the owner's poll loop
			last := ""
			for {
				select {
				case <-done:
					seen <- last
					return
				default:
				}
				if target, ok := m.TakeTarget(tk); ok {
					last = target
				}
			}
		}()
		w.goBurst(t)
		got := w.result(t)
		close(done)
		last := <-seen
		if got != "OK" {
			t.Fatalf("round %d: writer: %s", round, got)
		}
		if target, ok := m.TakeTarget(tk); ok {
			last = target
		}
		if last != "final" {
			t.Fatalf("round %d: the owner last saw target %q, want the final request", round, last)
		}
	}
	if left := strayFiles(t, m.Dir); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}
