package ledgerindex

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Stats describes the index of a ledger, for `heavyq doctor`.
type Stats struct {
	Dir string `json:"dir"`
	// State is "fresh" (covers the whole ledger), "stale" (the ledger has
	// grown past it: readers fall back to the tail until the next run or a
	// reindex) or "missing" (no usable meta.json).
	State   string `json:"state"`
	Lanes   int    `json:"lanes"`
	Trees   int    `json:"trees"`
	Bytes   int64  `json:"bytes"` // all index files
	Records int64  `json:"records"`
	Evicted bool   `json:"evicted"`
}

// StatsOf reads the index of the ledger at path.
func StatsOf(path string) (Stats, error) {
	dir := Dir(path)
	st := Stats{Dir: dir, State: "missing"}
	m, _, fresh := readFresh(path)
	if m.V == schema {
		st.State, st.Records, st.Evicted = "stale", m.Seq, m.Evicted
		if fresh {
			st.State = "fresh"
		}
	}
	for _, sub := range []struct {
		name string
		n    *int
	}{{"lanes", &st.Lanes}, {"trees", &st.Trees}} {
		entries, err := readDirIfExists(filepath.Join(dir, sub.name))
		if err != nil {
			return st, err
		}
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				*sub.n++
				st.Bytes += info.Size()
			}
		}
	}
	for _, f := range []string{"meta.json", "manifest.json"} {
		if info, err := os.Stat(filepath.Join(dir, f)); err == nil {
			st.Bytes += info.Size()
		}
	}
	return st, nil
}

// readDirIfExists lists dir; a missing directory is empty.
func readDirIfExists(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}
