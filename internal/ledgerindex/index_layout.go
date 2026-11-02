// Package ledgerindex keeps small, rebuildable indexes next to the run ledger so
// that hot paths (gate status, hooks, why, the Tower) never scan it.
//
// The ledger (internal/history) stays the only truth. Beside it, in an index
// directory, three kinds of JSON files are derived from it:
//
//   - lanes/<hash>.json: the last LaneRecords records of one lane;
//   - trees/<hash>.json: the conclusive runs of one (repo, tree), newest
//     TreeRunsKept of them, and per run_id the latest exit with pass/fail counts;
//   - meta.json (what the index covers: ledger file and byte size) and
//     manifest.json (what exists, for eviction; only writers read it).
//
// Append is the one write path: under a lock it brings the index up to the end
// of the ledger, appends the record, and indexes it. The index is fresh while
// meta.json's size equals the ledger's size; any other append (an older heavyq,
// a lock timeout) makes it stale, which readers see and answer from a bounded
// ReadTail instead, and the next Append catches up from the recorded offset.
// `heavyq doctor --reindex` rebuilds everything from the ledger.
package ledgerindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const schema = 1

// Bounds. They are variables only so tests can make eviction happen with few
// records; a rebuild with other values simply yields a different index.
var (
	// LaneRecords is how many records a lane file keeps.
	LaneRecords = 50
	// TreeRunsKept is how many conclusive runs a tree file keeps. A tree with more
	// is answered from the ledger tail instead (see TreeRuns).
	TreeRunsKept = 32
	// maxOutcomes bounds the per-run_id summaries of a tree.
	maxOutcomes = 64
	// maxLanes and maxTrees bound the number of files; the least recently
	// touched is evicted first.
	maxLanes = 128
	maxTrees = 256
)

// lockWait is how long Append waits for another writer before it gives up on
// the index (the ledger is still written).
const lockWait = 5 * time.Second

// monthlyRe mirrors history's rotated file names.
var monthlyRe = regexp.MustCompile(`^history-\d{4}-\d{2}\.jsonl$`)

// Dir is where the index of the ledger at path lives: "index" beside a rotated
// ledger, "<file>.index" beside a pinned one ($HEAVYQ_HISTORY), so two pinned
// files in one directory never share an index.
func Dir(ledgerPath string) string {
	if monthlyRe.MatchString(filepath.Base(ledgerPath)) {
		return filepath.Join(filepath.Dir(ledgerPath), "index")
	}
	return ledgerPath + ".index"
}

// meta says what the index covers. Size is the byte size of Ledger when the
// index was last brought up to date.
type meta struct {
	V       int    `json:"v"`
	Ledger  string `json:"ledger"`
	Size    int64  `json:"size"`
	Seq     int64  `json:"seq"`     // records applied; the clock of eviction
	Evicted bool   `json:"evicted"` // some lane or tree has been evicted: a missing file proves nothing
}

// manifest lists the files, each with the seq of the record that touched it last.
type manifest struct {
	Lanes map[string]int64 `json:"lanes"`
	Trees map[string]int64 `json:"trees"`
}

func laneFile(dir, lane string) string { return filepath.Join(dir, "lanes", sum(lane)+".json") }
func treeFile(dir, repo, tree string) string {
	return filepath.Join(dir, "trees", sum(repo, tree)+".json")
}

// sum names the file of a key: a hash, so no ledger value ever becomes a path.
func sum(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:12])
}

// writeJSON replaces path with the JSON of v atomically (temp file and rename),
// so a reader sees the old file or the new one, never half of one.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readJSON decodes the file at path into v. A missing file is fs.ErrNotExist.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// lock takes the writers' lock of dir, waiting up to lockWait. flock is
// released by the kernel when the process dies, so a killed writer never
// blocks the next one.
func lock(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("index lock: %w", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// fileSize is the size of path, 0 when it does not exist.
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
