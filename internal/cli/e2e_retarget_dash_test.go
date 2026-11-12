package cli

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The dashboard's control API moves a waiting ticket the way `heavyq retarget`
// does, with the same refusals: a real `heavyq dash --control` process, a real
// waiting ticket.

var dashLink = regexp.MustCompile(`(http://127\.0\.0\.1:[0-9]+)/#token=([0-9a-f]+)`)

func TestE2EDashboardMovesAWaitingTicket(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	dash := h.start("dash", "--control")
	var base, token string
	h.until("the dashboard link", func() bool {
		m := dashLink.FindStringSubmatch(dash.out.String())
		if m != nil {
			base, token = m[1], m[2]
		}
		return m != nil
	})
	post := func(id, body string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, base+"/api/v1/tickets/"+id+"/target", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}

	hold := h.start("3", "--remote", "box", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	moved := h.start("3", "--remote", "box", "--lane", "moved", "--", "true")
	h.waiting(1)
	id := h.snapshot().Waiting[0].Name
	for body, want := range map[string]int{`{"remote":"ghost"}`: 400, `{}`: 400, `nope`: 400} {
		if code := post(id, body); code != want {
			t.Fatalf("POST target %s = %d, want %d", body, code, want)
		}
	}
	if got := h.waitingTarget("moved"); got != "box" {
		t.Fatalf("target after refused requests = %q, want box", got)
	}
	if code := post(id, `{"remote":"local"}`); code != http.StatusAccepted {
		t.Fatalf("POST target local = %d, want 202", code)
	}
	if rc := moved.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("moved ticket rc=%d\n%s", rc, moved.out)
	}
	if got := h.machineOfLane("moved"); got != "local" {
		t.Fatalf("the moved ticket ran on %q, want local", got)
	}
	h.touch("go")
	if rc := hold.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("holder rc=%d\n%s", rc, hold.out)
	}
}
