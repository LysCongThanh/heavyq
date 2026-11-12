package dash

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/LysCongThanh/heavyq/internal/control"
)

// retarget files a move of a waiting ticket to other machines, the request of
// `heavyq retarget`: POST tickets/{id}/target with {"remote": "WHERE"}. The
// ticket's process applies it on its next poll (202). The same checks as the
// command run first (Options.CheckTarget); a server wired without them refuses
// every move rather than file one unchecked.
func (s *Server) retarget(w http.ResponseWriter, r *http.Request) {
	id, running, ok := s.ticketFor(w, r)
	if !ok {
		return
	}
	if running {
		writeError(w, http.StatusConflict, "the ticket is already running; only waiting tickets move")
		return
	}
	var body struct {
		Remote string `json:"remote"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&body); err != nil || body.Remote == "" {
		writeError(w, http.StatusBadRequest, "body must be {\"remote\": \"local|auto|<remote>|a,b\"}")
		return
	}
	if s.opts.CheckTarget == nil {
		writeError(w, http.StatusNotImplemented, "moving tickets is not available on this server")
		return
	}
	if err := s.opts.CheckTarget(id, body.Remote); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.opts.Mailbox.RequestTarget(id, body.Remote); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, control.ErrBadName) || errors.Is(err, control.ErrBadTarget) {
			code = http.StatusBadRequest
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}
