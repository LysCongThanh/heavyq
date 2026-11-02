package ledgerindex

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// laneDoc is a lane file: the newest records of one lane, oldest first.
type laneDoc struct {
	Lane    string           `json:"lane"`
	Records []history.Record `json:"records"`
}

// outcome summarises the runs of one run_id on a tree: the latest exit and
// when, and how many real runs (cache replays excluded) passed and failed.
type outcome struct {
	Key  string    `json:"key"` // run_id; "cmd:<cmd_hash or cmd>" on records without one
	Exit int       `json:"exit"`
	At   time.Time `json:"at"`
	Pass int       `json:"pass"`
	Fail int       `json:"fail"`
}

// treeDoc is a tree file: the conclusive runs on one (repo, tree), oldest
// first, newest TreeRuns kept, with Dropped counting the older ones that were
// cut (then Runs cannot decide a proof alone).
type treeDoc struct {
	Repo     string           `json:"repo"`
	Tree     string           `json:"tree"`
	Runs     []history.Record `json:"runs"`
	Dropped  int              `json:"dropped,omitempty"`
	Outcomes []outcome        `json:"outcomes"`
}

// state is the index in memory: the meta and manifest plus the lane and tree
// documents loaded or created since it was opened. It reads files lazily from
// dir (empty dir: nothing on disk counts, as in a rebuild) and writes only what
// changed. apply is a pure function of the state and the record, so an index
// built record by record and one rebuilt from the ledger are identical.
type state struct {
	dir   string
	meta  meta
	man   manifest
	lanes map[string]*laneDoc // by file name (sum of the lane)
	trees map[string]*treeDoc // by file name (sum of repo and tree)
	dirty map[string]bool     // lane and tree names to write
	gone  []string            // files to delete (evicted)
	wipe  bool                // rewrite everything: delete the old files first
}

func newState(dir string, m meta, man manifest) *state {
	if man.Lanes == nil {
		man.Lanes = map[string]int64{}
	}
	if man.Trees == nil {
		man.Trees = map[string]int64{}
	}
	return &state{dir: dir, meta: m, man: man, lanes: map[string]*laneDoc{}, trees: map[string]*treeDoc{}, dirty: map[string]bool{}}
}

// openState loads meta and manifest from dir. Any problem (missing, corrupt,
// another schema) is an error: the caller rebuilds.
func openState(dir string) (*state, error) {
	var m meta
	if err := readJSON(filepath.Join(dir, "meta.json"), &m); err != nil {
		return nil, err
	}
	if m.V != schema {
		return nil, fmt.Errorf("index schema %d is not supported", m.V)
	}
	var man manifest
	if err := readJSON(filepath.Join(dir, "manifest.json"), &man); err != nil {
		return nil, err
	}
	return newState(dir, m, man), nil
}

// emptyState is the start of a rebuild.
func emptyState() *state {
	s := newState("", meta{V: schema}, manifest{})
	s.wipe = true
	return s
}

// conclusive: the record is a run that went to its end at the root of a
// repository's work tree, which is what a proof of a tree can use.
func conclusive(r history.Record) bool {
	return r.Repo != "" && r.Tree != "" && r.Subdir == "" && r.Result == ""
}

// apply indexes one ledger record, in ledger order.
func (s *state) apply(r history.Record) error {
	s.meta.Seq++
	if r.Lane != "" {
		d, name, err := s.lane(r.Lane)
		if err != nil {
			return err
		}
		d.Records = append(d.Records, r)
		if len(d.Records) >= 2*LaneRecords { // amortised: cut to LaneRecords at flush
			d.Records = append([]history.Record(nil), d.Records[len(d.Records)-LaneRecords:]...)
		}
		s.touchLane(name)
	}
	if conclusive(r) {
		d, name, err := s.tree(r.Repo, r.Tree)
		if err != nil {
			return err
		}
		d.Runs = append(d.Runs, r)
		if len(d.Runs) >= 2*TreeRunsKept {
			d.cutRuns()
		}
		d.note(r)
		s.touchTree(name)
	}
	return nil
}

func (d *treeDoc) cutRuns() {
	if extra := len(d.Runs) - TreeRunsKept; extra > 0 {
		d.Dropped += extra
		d.Runs = append([]history.Record(nil), d.Runs[extra:]...)
	}
}

// note updates the per-run_id summary with r; the most recently updated key is last.
func (d *treeDoc) note(r history.Record) {
	key := r.RunID
	switch {
	case key != "":
	case r.CmdHash != "":
		key = "cmd:" + r.CmdHash
	default:
		key = "cmd:" + r.Cmd
	}
	o := outcome{Key: key}
	for i, x := range d.Outcomes {
		if x.Key == key {
			o = x
			d.Outcomes = append(d.Outcomes[:i], d.Outcomes[i+1:]...)
			break
		}
	}
	o.Exit, o.At = r.Exit, r.End
	if r.Cache != "hit" && r.Cache != "coalesced" {
		if r.Exit == 0 {
			o.Pass++
		} else {
			o.Fail++
		}
	}
	d.Outcomes = append(d.Outcomes, o)
	if len(d.Outcomes) > maxOutcomes {
		d.Outcomes = append([]outcome(nil), d.Outcomes[len(d.Outcomes)-maxOutcomes:]...)
	}
}

// lane returns the document of lane, loading it when the manifest has it.
func (s *state) lane(lane string) (*laneDoc, string, error) {
	name := sum(lane)
	if d, ok := s.lanes[name]; ok {
		return d, name, nil
	}
	d := &laneDoc{Lane: lane}
	if _, ok := s.man.Lanes[name]; ok && s.dir != "" {
		if err := readJSON(laneFile(s.dir, lane), d); err != nil {
			return nil, "", err
		}
	}
	s.lanes[name] = d
	return d, name, nil
}

func (s *state) tree(repo, tree string) (*treeDoc, string, error) {
	name := sum(repo, tree)
	if d, ok := s.trees[name]; ok {
		return d, name, nil
	}
	d := &treeDoc{Repo: repo, Tree: tree}
	if _, ok := s.man.Trees[name]; ok && s.dir != "" {
		if err := readJSON(treeFile(s.dir, repo, tree), d); err != nil {
			return nil, "", err
		}
	}
	s.trees[name] = d
	return d, name, nil
}
