package stance

import (
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
)

// Stats is `heavyq stance stats`: the measurement of every stance this
// repository's lanes have been in, from its events log and the ledger index.
func (e Env) Stats(cwd string) ([]StanceStats, error) {
	set, err := e.LoadSet(cwd)
	if err != nil {
		return nil, err
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return nil, err
	}
	events, err := store.Events()
	if err != nil {
		return nil, err
	}
	return Stats(Activations(events), func(lane string) []history.Record {
		recs, _, _ := ledgerindex.LaneRuns(e.Ledger, lane) // an unreadable ledger means "no proof seen"
		return recs
	}), nil
}
