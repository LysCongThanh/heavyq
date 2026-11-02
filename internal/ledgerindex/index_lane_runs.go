package ledgerindex

import (
	"errors"
	"io/fs"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// LaneRuns returns the newest records of lane, oldest first: the lane's file
// of the index (at most LaneRecords), or, when the index cannot answer, the
// lane's records among the newest TailWindow of the ledger. Stances read it to
// look for a failure that repeats across trees, and to time a proof.
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
	out := recs[:0]
	for _, r := range recs {
		if r.Lane == lane {
			out = append(out, r)
		}
	}
	return out, FromTail, err
}
