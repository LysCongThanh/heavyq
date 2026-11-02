package ledgerindex

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Source says where a read was answered from.
type Source string

const (
	// FromIndex: read from the index files (one or a few small files).
	FromIndex Source = "index"
	// FromTail: the index was missing, stale or could not say, so the answer
	// comes from the newest TailWindow records of the ledger.
	FromTail Source = "tail"
)

// TailWindow is how many records the fallback reads from the end of the
// ledger: about 5 ms, and a proof older than that window is reported as
// missing, never guessed.
const TailWindow = 2000

// readFresh returns the meta of the index at ledgerPath when it covers the
// whole ledger: meta.json exists and the ledger has exactly the size it was
// indexed at (or has rotated since without a record yet). Any doubt is false.
func readFresh(ledgerPath string) (meta, string, bool) {
	dir := Dir(ledgerPath)
	var m meta
	if err := readJSON(filepath.Join(dir, "meta.json"), &m); err != nil || m.V != schema {
		return m, dir, false
	}
	head, err := fileSize(ledgerPath)
	if err != nil {
		return m, dir, false
	}
	if m.Ledger == filepath.Base(ledgerPath) {
		return m, dir, head == m.Size
	}
	old, err := fileSize(filepath.Join(filepath.Dir(ledgerPath), m.Ledger))
	return m, dir, err == nil && head == 0 && old == m.Size
}

// TreeRuns returns the ledger records that can decide a proof of (repo, tree),
// oldest first: the conclusive runs on that tree. proof.Evaluate takes them as
// the ledger. From the index it is one small file, and the same records a full
// scan would give. The index cannot answer, and the newest TailWindow records
// of the ledger are filtered instead, when it is stale or missing, when the
// tree has more runs than the index keeps, or when the tree has no file while
// the index has evicted some (it may have been evicted).
func TreeRuns(ledgerPath, repo, tree string) ([]history.Record, Source, error) {
	if m, dir, ok := readFresh(ledgerPath); ok {
		var d treeDoc
		err := readJSON(treeFile(dir, repo, tree), &d)
		switch {
		case err == nil && d.Dropped == 0 && d.Repo == repo && d.Tree == tree:
			return d.Runs, FromIndex, nil
		case errors.Is(err, fs.ErrNotExist) && !m.Evicted:
			return nil, FromIndex, nil // no conclusive run on this tree, ever
		}
	}
	recs, err := history.ReadTail(ledgerPath, history.TailLimit{Max: TailWindow})
	out := recs[:0]
	for _, r := range recs {
		if r.Repo == repo && r.Tree == tree && conclusive(r) {
			out = append(out, r)
		}
	}
	return out, FromTail, err
}

// LastOfLane returns the newest record of lane, or nil when it has none (in
// the index, or in the fallback window).
func LastOfLane(ledgerPath, lane string) (*history.Record, Source, error) {
	if m, dir, ok := readFresh(ledgerPath); ok {
		var d laneDoc
		err := readJSON(laneFile(dir, lane), &d)
		switch {
		case err == nil && d.Lane == lane && len(d.Records) > 0:
			return &d.Records[len(d.Records)-1], FromIndex, nil
		case errors.Is(err, fs.ErrNotExist) && !m.Evicted:
			return nil, FromIndex, nil
		}
	}
	recs, err := history.ReadTail(ledgerPath, history.TailLimit{Max: TailWindow})
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Lane == lane {
			return &recs[i], FromTail, err
		}
	}
	return nil, FromTail, err
}

// Recent returns the records of every lane that ended at or after since,
// oldest first. From the index that is each lane's last LaneRecords at most
// (lane files not written since are not even opened); the fallback reads the
// ledger tail back to since, at most TailWindow records.
func Recent(ledgerPath string, since time.Time) ([]history.Record, Source, error) {
	if _, dir, ok := readFresh(ledgerPath); ok {
		if recs, err := recentFromLanes(dir, since); err == nil {
			return recs, FromIndex, nil
		}
	}
	recs, err := history.ReadTail(ledgerPath, history.TailLimit{Max: TailWindow, Since: since})
	return recs, FromTail, err
}

func recentFromLanes(dir string, since time.Time) ([]history.Record, error) {
	entries, err := readDirIfExists(filepath.Join(dir, "lanes"))
	if err != nil {
		return nil, err
	}
	var out []history.Record
	for _, e := range entries {
		// A lane file is rewritten whenever a record is added, after that
		// record ended: an older file cannot hold a record in the window.
		if info, err := e.Info(); err != nil || info.ModTime().Before(since.Add(-time.Minute)) {
			continue
		}
		var d laneDoc
		if err := readJSON(filepath.Join(dir, "lanes", e.Name()), &d); err != nil {
			return nil, err
		}
		for _, r := range d.Records {
			if !r.End.Before(since) {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].End.Equal(out[j].End) {
			return out[i].End.Before(out[j].End)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
