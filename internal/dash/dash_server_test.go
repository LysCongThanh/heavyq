package dash

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/control"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

const waitingID = "3-01762011288000000000-50122-wt-billing"
const runningID = "2-01762011200000000000-48213-wt-search"

type fixture struct {
	t    *testing.T
	base string
	tok  string
	box  control.Mailbox
}

func start(t *testing.T, ctl bool) *fixture {
	t.Helper()
	box := control.Mailbox{Dir: t.TempDir()}
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	s, err := New(Options{
		Control: ctl, Interval: time.Second, Version: "test", Host: "box", Mailbox: box,
		Snapshot: func() (snapshot.Snapshot, error) {
			return snapshot.Snapshot{Schema: 1, Host: "box", Running: []snapshot.Running{}}, nil
		},
		History: func(int) ([]history.Record, error) {
			return []history.Record{{End: now.Add(-time.Minute), Lane: "a", Cmd: "TOKEN=xyz make", RunSecs: 3}}, nil
		},
		Ticket: func(id string) (bool, bool) {
			return id == waitingID || id == runningID, id == runningID
		},
		Machine: func(n string) bool { return n == "local" },
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: s.Handler()}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	return &fixture{t: t, base: "http://" + ln.Addr().String(), tok: s.Token(), box: box}
}

func (f *fixture) do(method, path, body string, hdr map[string]string) (int, string) {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.tok)
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestSecurityChecks(t *testing.T) {
	f := start(t, true)
	if code, _ := f.do("GET", "/api/v1/meta", "", map[string]string{"Authorization": "Bearer nope"}); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _ := f.do("GET", "/api/v1/meta", "", map[string]string{"Host": "attacker.example:80"}); code != 421 {
		t.Fatalf("foreign Host (DNS rebinding): %d", code)
	}
	if code, _ := f.do("POST", "/api/v1/machines/local/pause", "", map[string]string{"Origin": "http://attacker.example"}); code != 403 {
		t.Fatalf("cross-origin POST: %d", code)
	}
	if f.box.Paused("local") {
		t.Fatal("cross-origin request had an effect")
	}
	res, err := http.Get(f.base + "/")
	if err != nil || res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("page: %v %v", res, err)
	}
	res.Body.Close()
	if _, err := (&Server{}).Listen("0.0.0.0:0"); err == nil {
		t.Fatal("non-loopback listen must be refused")
	}
}

func TestReadOnlyRefusesControl(t *testing.T) {
	f := start(t, false)
	if code, body := f.do("GET", "/api/v1/meta", "", nil); code != 200 || !strings.Contains(body, `"control":false`) {
		t.Fatalf("meta: %d %s", code, body)
	}
	if code, _ := f.do("POST", "/api/v1/tickets/"+waitingID+"/cancel", "", nil); code != 403 {
		t.Fatalf("cancel in read-only mode: %d", code)
	}
	if f.box.CancelRequested(waitingID) {
		t.Fatal("read-only server wrote a request")
	}
}

func TestControlActions(t *testing.T) {
	f := start(t, true)
	cases := []struct {
		path, body string
		want       int
	}{
		{"/api/v1/tickets/" + waitingID + "/prio", `{"prio":2}`, 202},
		{"/api/v1/tickets/" + runningID + "/prio", `{"prio":2}`, 409},
		{"/api/v1/tickets/" + waitingID + "/prio", `{"prio":12}`, 400},
		{"/api/v1/tickets/" + waitingID + "/prio", `nope`, 400},
		{"/api/v1/tickets/..%2Fx/cancel", ``, 400},
		{"/api/v1/tickets/3-01762011288000000000-1-gone/cancel", ``, 404},
		{"/api/v1/tickets/" + runningID + "/cancel", ``, 202},
		{"/api/v1/machines/local/pause", ``, 200},
		{"/api/v1/machines/nowhere/pause", ``, 404},
	}
	for _, c := range cases {
		if code, body := f.do("POST", c.path, c.body, map[string]string{"Content-Type": "application/json"}); code != c.want {
			t.Errorf("POST %s %s = %d %s, want %d", c.path, c.body, code, body, c.want)
		}
	}
	if p, ok := f.box.TakePrio(waitingID); !ok || p != 2 {
		t.Fatal("prio request not written")
	}
	if !f.box.CancelRequested(runningID) || !f.box.Paused("local") {
		t.Fatal("cancel / pause not written")
	}
}

func TestDataEndpoints(t *testing.T) {
	f := start(t, false)
	code, body := f.do("GET", "/api/v1/history?n=5", "", nil)
	if code != 200 || !strings.Contains(body, "TOKEN=***") || strings.Contains(body, "xyz") {
		t.Fatalf("history: %d %s", code, body)
	}
	code, body = f.do("GET", "/api/v1/insights?window=1h", "", nil)
	var in snapshot.Insights
	if code != 200 || json.Unmarshal([]byte(body), &in) != nil || in.Runs != 1 || len(in.Buckets) != insightBuckets {
		t.Fatalf("insights: %d %s", code, body)
	}
	if code, _ := f.do("GET", "/api/v1/insights?window=2y", "", nil); code != 400 {
		t.Fatal("bad window accepted")
	}
	req, _ := http.NewRequest("GET", f.base+"/api/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+f.tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	var lines []string
	for len(lines) < 4 && sc.Scan() { // two events: initial + first produced
		if l := sc.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 4 || lines[0] != "event: snapshot" || !strings.HasPrefix(lines[1], `data: {"schema":1`) {
		t.Fatalf("stream = %q", lines)
	}
}
