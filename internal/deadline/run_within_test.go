package deadline

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunWithinReturnsResultInTime(t *testing.T) {
	v, err := RunWithin(time.Second, func(context.Context) (string, error) { return "ok", nil })
	if v != "ok" || err != nil {
		t.Fatalf("got %q, %v", v, err)
	}
	boom := errors.New("boom")
	v, err = RunWithin(time.Second, func(context.Context) (string, error) { return "partial", boom })
	if v != "partial" || !errors.Is(err, boom) {
		t.Fatalf("handler error must pass through unchanged: %q, %v", v, err)
	}
}

// A handler that misses the deadline is abandoned: the caller gets the zero
// value and ErrExceeded at about the deadline, not when the handler finishes.
func TestRunWithinFailsOpenOnDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	v, err := RunWithin(30*time.Millisecond, func(ctx context.Context) (int, error) {
		<-release // ignores ctx on purpose: a handler stuck in a syscall
		return 7, nil
	})
	if v != 0 || !errors.Is(err, ErrExceeded) {
		t.Fatalf("got %d, %v", v, err)
	}
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("returned after %v, not at the deadline", el)
	}
}

func TestRunWithinCancelsContextAtDeadline(t *testing.T) {
	stopped := make(chan error, 1)
	_, err := RunWithin(20*time.Millisecond, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		stopped <- ctx.Err()
		return 0, ctx.Err()
	})
	if err == nil {
		t.Fatal("want an error")
	}
	select {
	case got := <-stopped:
		if !errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("handler saw %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler never saw the cancellation")
	}
}

func TestRunWithinContainsPanic(t *testing.T) {
	v, err := RunWithin(time.Second, func(context.Context) (int, error) { panic("bad ledger") })
	if v != 0 || !errors.Is(err, ErrPanic) {
		t.Fatalf("got %d, %v", v, err)
	}
}

func TestRunWithinNoTimeFailsOpenWithoutRunning(t *testing.T) {
	ran := false
	for _, d := range []time.Duration{0, -time.Second} {
		_, err := RunWithin(d, func(context.Context) (int, error) { ran = true; return 1, nil })
		if !errors.Is(err, ErrExceeded) {
			t.Fatalf("%v: %v", d, err)
		}
	}
	if ran {
		t.Fatal("handler ran with no time to run")
	}
}
