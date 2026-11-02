package ledgerindex

import (
	"errors"
	"io/fs"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// LaneRuns returns the newest records of lane, oldest first: at most
// LaneRecords of them from the index (one small file), or the lane's share of
// the newest TailWindow ledger records when the index cannot answer. It is the
// read behind detectors that need a short run of one lane (internal/mentor).
// A lane without records yields nil.
func LaneRuns(ledgerPath, lane string) ([]history.Record, Source, error) {
	if m, dir, ok := readFresh(ledgerPath); ok {
		var d laneDoc
		err := readJSON(laneFile(dir, lane), &d)
		switch {
		case err == nil && d.Lane == lane:
			return d.Records, FromIndex, nil
		case errors.Is(err, fs.ErrNotExist) && !m.Evicted:
			return nil, FromIndex, nil
		}
	}
	recs, err := history.ReadTail(ledgerPath, history.TailLimit{Max: TailWindow})
	var out []history.Record
	for _, r := range recs {
		if r.Lane == lane {
			out = append(out, r)
		}
	}
	return out, FromTail, err
}
