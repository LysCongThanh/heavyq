package app

import (
	"context"
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// Source builds one snapshot (cli.buildSnapshot in heavyq top).
type Source func() (snapshot.Snapshot, error)

// minGap is the shortest pause between two builds, so a slow source never spins.
const minGap = 200 * time.Millisecond

// result is the outcome of one build.
type result struct {
	snap snapshot.Snapshot
	err  error
}

// startFetcher builds snapshots on one goroutine, so two builds never
// overlap: the next build starts when the previous finished and at least
// interval passed since it started, or at once when refresh fires. Only the
// newest result is kept: a slow reader never queues stale snapshots.
func startFetcher(ctx context.Context, src Source, interval time.Duration, refresh <-chan struct{}) <-chan result {
	out := make(chan result, 1)
	go func() {
		defer close(out)
		for {
			started := time.Now()
			r := build(src)
			select { // replace an unread result with the newer one
			case <-out:
			default:
			}
			out <- r
			wait := time.NewTimer(max(interval-time.Since(started), minGap))
			select {
			case <-ctx.Done():
				wait.Stop()
				return
			case <-refresh:
				wait.Stop()
			case <-wait.C:
			}
		}
	}()
	return out
}

// build runs src and turns a panic into an error: a bug in one snapshot must
// not leave the user's terminal raw.
func build(src Source) (r result) {
	defer func() {
		if p := recover(); p != nil {
			r = result{err: fmt.Errorf("snapshot build panicked: %v", p)}
		}
	}()
	s, err := src()
	return result{snap: s, err: err}
}
