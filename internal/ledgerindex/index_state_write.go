package ledgerindex

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// touchLane and touchTree mark name as the newest of its kind and evict the
// oldest while there are too many. Eviction depends only on the records seen
// so far, which is what makes a rebuild reproduce the index.
func (s *state) touchLane(name string) {
	s.man.Lanes[name] = s.meta.Seq
	s.dirty[name] = true
	for len(s.man.Lanes) > maxLanes {
		old := oldest(s.man.Lanes, name)
		delete(s.man.Lanes, old)
		delete(s.lanes, old)
		delete(s.dirty, old)
		s.gone = append(s.gone, filepath.Join("lanes", old+".json"))
		s.meta.Evicted = true
	}
}

func (s *state) touchTree(name string) {
	s.man.Trees[name] = s.meta.Seq
	s.dirty[name] = true
	for len(s.man.Trees) > maxTrees {
		old := oldest(s.man.Trees, name)
		delete(s.man.Trees, old)
		delete(s.trees, old)
		delete(s.dirty, old)
		s.gone = append(s.gone, filepath.Join("trees", old+".json"))
		s.meta.Evicted = true
	}
}

// oldest is the key of m with the smallest seq, other than keep.
func oldest(m map[string]int64, keep string) string {
	best, bestSeq := "", int64(-1)
	for k, seq := range m {
		if k != keep && (bestSeq < 0 || seq < bestSeq) {
			best, bestSeq = k, seq
		}
	}
	return best
}

// flush writes what changed (all of it after a rebuild) and the manifest,
// then meta last: meta is what makes the files count.
func (s *state) flush(dir string) error {
	if s.wipe {
		for _, f := range []string{"meta.json", "manifest.json"} {
			if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		for _, sub := range []string{"lanes", "trees"} {
			if err := os.RemoveAll(filepath.Join(dir, sub)); err != nil {
				return err
			}
		}
	}
	for _, f := range s.gone { // before the writes: an evicted name may come back
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for name, d := range s.lanes {
		if s.dirty[name] || s.wipe {
			if _, kept := s.man.Lanes[name]; !kept {
				continue
			}
			if len(d.Records) > LaneRecords {
				d.Records = append([]history.Record(nil), d.Records[len(d.Records)-LaneRecords:]...)
			}
			if err := writeJSON(laneFile(dir, d.Lane), d); err != nil {
				return err
			}
		}
	}
	for name, d := range s.trees {
		if s.dirty[name] || s.wipe {
			if _, kept := s.man.Trees[name]; !kept {
				continue
			}
			d.cutRuns()
			if err := writeJSON(treeFile(dir, d.Repo, d.Tree), d); err != nil {
				return err
			}
		}
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), s.man); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "meta.json"), s.meta)
}
