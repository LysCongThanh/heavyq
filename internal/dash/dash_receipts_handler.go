package dash

import (
	"net/http"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/receipts"
)

// receiptsResponse is served at /api/v1/receipts: the exact counts for the
// panel and the rounded card the copy button puts on the clipboard.
type receiptsResponse struct {
	Window   string            `json:"window"`
	Receipts receipts.Receipts `json:"receipts"`
	Share    receipts.Share    `json:"share"`
}

// receiptsHandler serves GET /api/v1/receipts?window=day|week|month (week by
// default), read through the same bounded tail as the insights.
func (s *Server) receiptsHandler(w http.ResponseWriter, r *http.Request) {
	name, d, err := receipts.ParseWindow(r.URL.Query().Get("window"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to := s.opts.Now()
	var recs []history.Record
	if s.opts.HistorySince != nil {
		recs, err = s.opts.HistorySince(to.Add(-d))
	} else {
		recs, err = s.opts.History(0)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rc := receipts.Compute(recs, to.Add(-d), to)
	writeJSON(w, http.StatusOK, receiptsResponse{Window: name, Receipts: rc,
		Share: receipts.NewShare(rc, s.opts.Version, name)})
}
