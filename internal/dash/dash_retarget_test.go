package dash

import (
	"errors"
	"testing"
)

// POST tickets/{id}/target files the same request as `heavyq retarget`, after
// the same checks (Options.CheckTarget); a server wired without them refuses.
func TestRetargetAction(t *testing.T) {
	f := start(t, true, func(o *Options) {
		o.CheckTarget = func(id, where string) error {
			if where != "local" {
				return errors.New("no machine " + where)
			}
			return nil
		}
	})
	hdr := map[string]string{"Content-Type": "application/json"}
	for _, c := range []struct {
		id, body string
		want     int
	}{
		{runningID, `{"remote":"local"}`, 409},
		{waitingID, `{"remote":"ghost"}`, 400},
		{waitingID, `{}`, 400},
		{waitingID, `nope`, 400},
		{"3-01762011288000000000-1-gone", `{"remote":"local"}`, 404},
		{waitingID, `{"remote":"local"}`, 202},
	} {
		if code, body := f.do("POST", "/api/v1/tickets/"+c.id+"/target", c.body, hdr); code != c.want {
			t.Errorf("POST %s target %s = %d %s, want %d", c.id, c.body, code, body, c.want)
		}
	}
	if w, ok := f.box.TakeTarget(waitingID); !ok || w != "local" {
		t.Fatalf("target request = %q %v, want local", w, ok)
	}

	ro := start(t, false)
	if code, _ := ro.do("POST", "/api/v1/tickets/"+waitingID+"/target", `{"remote":"local"}`, hdr); code != 403 {
		t.Fatalf("read-only server: %d, want 403", code)
	}
	bare := start(t, true) // no CheckTarget: nothing is moved unchecked
	if code, _ := bare.do("POST", "/api/v1/tickets/"+waitingID+"/target", `{"remote":"local"}`, hdr); code != 501 {
		t.Fatalf("server without checks: %d, want 501", code)
	}
	if _, ok := bare.box.TakeTarget(waitingID); ok {
		t.Fatal("an unchecked move was filed")
	}
}
