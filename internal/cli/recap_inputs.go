package cli

import (
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/recap"
)

// recapItems flattens the derived item rows into what the recap reads.
func recapItems(rows []itemRow) []recap.Item {
	out := make([]recap.Item, 0, len(rows))
	for _, r := range rows {
		s := r.Status
		out = append(out, recap.Item{ID: s.ID, State: s.State, Lane: s.Lane, Session: s.Session, Commit: s.Commit,
			Needs: s.Needs, Reason: s.Reason, Text: r.Item.Text, At: s.At, Expires: s.Expires, Done: s.Done})
	}
	return out
}

// recordsOfRepo keeps the ledger records of one repository (the ledger is
// shared by every repository on the machine). A record with no repository is
// not claimed by any.
func recordsOfRepo(recs []history.Record, repo string) []history.Record {
	if repo == "" {
		return nil
	}
	out := recs[:0:0]
	for _, r := range recs {
		if r.Repo == repo {
			out = append(out, r)
		}
	}
	return out
}
