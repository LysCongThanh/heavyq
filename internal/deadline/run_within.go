// Package deadline bounds the time a hook handler may take. Hooks run on every
// tool call of an agent, so heavyq promises a budget (20 ms for the per-call
// events) and fails open when it cannot keep it: no output, exit 0, never a
// blocked agent.
package deadline

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// HookBudget is the default deadline of a per-tool-call hook handler. It leaves
// 5 ms of the 20 ms promise for process start and output.
const HookBudget = 15 * time.Millisecond

// Errors RunWithin returns when fn gave no answer. Both mean "fail open".
var (
	// ErrExceeded: fn did not finish within the deadline.
	ErrExceeded = errors.New("deadline exceeded")
	// ErrPanic: fn panicked; the panic is contained.
	ErrPanic = errors.New("handler panicked")
)

type result[T any] struct {
	v   T
	err error
}

// RunWithin runs fn and waits at most d for it, with fail-open semantics: when
// fn returns in time, RunWithin returns its value and error unchanged; when it
// does not (or panics, or d <= 0 leaves no time at all) RunWithin returns the
// zero T and an error that matches ErrExceeded (or ErrPanic) with errors.Is.
// The caller treats any error as "say nothing".
//
// fn receives a context that is cancelled at the deadline. A handler that checks
// it stops early; one that does not is abandoned, not killed: its goroutine
// runs on until it returns or the process exits, which for a hook is
// immediately after. fn must therefore not write anything the caller could
// also write (no output, no shared state) once the deadline passes.
func RunWithin[T any](d time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	if d <= 0 {
		return zero, fmt.Errorf("%w: no time left (%v)", ErrExceeded, d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan result[T], 1) // buffered: an abandoned fn never blocks on send
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result[T]{err: fmt.Errorf("%w: %v", ErrPanic, p)}
			}
		}()
		v, err := fn(ctx)
		done <- result[T]{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		return zero, fmt.Errorf("%w after %v", ErrExceeded, d)
	}
}
