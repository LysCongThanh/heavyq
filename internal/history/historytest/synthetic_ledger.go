// Package historytest generates synthetic run ledgers for tests and
// benchmarks of the ledger readers (tail reader, indexes, hook latency). It is
// imported by tests only.
package historytest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Options shapes a synthetic ledger: records evenly spread over Span, ending
// at End. The defaults mirror a busy fleet: about a month, 12 lanes, a new
// tree every 5 runs, one run in 8 failing, lines of roughly 550 bytes.
type Options struct {
	N     int           // records (default 100000)
	Span  time.Duration // default 30 days
	End   time.Time     // default now
	Lanes int           // default 12
}

// Repo is the repository id every generated record with an even lane number
// carries; odd lanes use Repo2.
const (
	Repo  = "repo0000000000000a"
	Repo2 = "repo0000000000000b"
)

func hash(parts ...any) string {
	h := sha256.Sum256([]byte(fmt.Sprint(parts...)))
	return hex.EncodeToString(h[:20])
}

// Filled returns o with the defaults in place. Set End to get the same records
// from Filled twice.
func (o Options) Filled() Options {
	if o.N <= 0 {
		o.N = 100000
	}
	if o.Span <= 0 {
		o.Span = 30 * 24 * time.Hour
	}
	if o.End.IsZero() {
		o.End = time.Now()
	}
	if o.Lanes <= 0 {
		o.Lanes = 12
	}
	return o
}

// Write writes the ledger to path (one JSON line per record, oldest first, the
// way history.Append would) and returns the newest record. It does not use
// history.Append, which opens the file once per record: too slow for 100k.
func Write(path string, o Options) (history.Record, error) {
	o = o.Filled()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return history.Record{}, err
	}
	f, err := os.Create(path)
	if err != nil {
		return history.Record{}, err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	enc := json.NewEncoder(w)
	var last history.Record
	for i := 0; i < o.N; i++ {
		last = Record(i, o)
		if err := enc.Encode(last); err != nil {
			f.Close()
			return last, err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return last, err
	}
	return last, f.Close()
}

// Record is the i-th record of the ledger Write produces for o.
func Record(i int, o Options) history.Record {
	o = o.Filled()
	lane := i % o.Lanes
	repo := Repo
	if lane%2 == 1 {
		repo = Repo2
	}
	tree := hash("tree", lane, i/5)
	cmd := fmt.Sprintf("sh -c go test -race -count=1 ./internal/pkg%d/... && go vet ./internal/pkg%d/...", i%4, i%4)
	rec := history.Record{
		V:        history.SchemaVersion,
		ID:       fmt.Sprintf("%026d", i),
		End:      o.End.Add(-time.Duration(int64(o.N-1-i) * int64(o.Span) / int64(o.N))),
		Machine:  "local",
		Lane:     fmt.Sprintf("lane-%02d", lane),
		Prio:     5,
		WaitSecs: float64(i%7) * 1.5,
		RunSecs:  float64(10 + i%90),
		Cmd:      cmd,
		CmdHash:  hash("cmd", i%4),
		RunID:    hash("run", tree, i%4),
		Session:  fmt.Sprintf("session-%02d", lane),
		Gate:     "ready",
		Repo:     repo,
		Worktree: fmt.Sprintf("/home/dev/work/project-%02d", lane),
		Commit:   hash("commit", lane, i/5),
		Tree:     tree,
		Cores:    16, Load1: 3.2, FreeMB: 20000, MaxLoad: 12, JobMB: 2048,
		CPUSecs: 40, MaxRSSMB: 900,
	}
	if i%8 == 7 {
		rec.Exit = 1
	}
	return rec
}
