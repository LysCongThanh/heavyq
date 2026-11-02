package cli

import (
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/proof"
)

// proofRecords returns the ledger records that decide a proof of subj: the
// conclusive runs on its repository and tree, oldest first. They come from the
// tree's index file, or, when the index cannot answer, from the newest
// ledgerindex.TailWindow records: never from a scan of the whole ledger, so a
// hot path (gate status, why, the Tower, hooks) stays within its time budget.
// A tree that cannot be identified has no records.
func proofRecords(subj proof.Subject) ([]history.Record, error) {
	if subj.Tree == "" {
		return nil, nil
	}
	recs, _, err := ledgerindex.TreeRuns(history.DefaultPath(), subj.Repo, subj.Tree)
	return recs, err
}
