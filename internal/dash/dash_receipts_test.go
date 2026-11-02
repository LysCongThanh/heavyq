package dash

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// receiptsServer serves a ledger the API reads through HistorySince, as heavyq
// dash does, and records the bound it was asked for.
func receiptsServer(t *testing.T, recs []history.Record, since *time.Time) (*Server, http.Handler) {
	t.Helper()
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	s, err := New(Options{
		Interval: time.Second, Version: "9.9.9", Host: "box",
		Snapshot: func() (snapshot.Snapshot, error) { return snapshot.Snapshot{Schema: 1}, nil },
		History:  func(int) ([]history.Record, error) { return recs, nil },
		HistorySince: func(t time.Time) ([]history.Record, error) {
			*since = t
			return recs, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	s.port = "4242"
	return s, s.Handler()
}

func getReceipts(s *Server, h http.Handler, query, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "http://127.0.0.1:4242/api/v1/receipts"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestReceiptsAPI(t *testing.T) {
	end := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	recs := []history.Record{
		{End: end, Machine: "local", Lane: "lane-canary", Cmd: "cmd-canary", WaitSecs: 30, RunID: "x"},
		{End: end, Machine: "local", Lane: "lane-canary", Cmd: "cmd-canary", Cache: "hit"},
		{End: end, Machine: "far-box", Lane: "other", Cmd: "cmd-canary", Pools: "pg=pg1"},
	}
	var since time.Time
	s, h := receiptsServer(t, recs, &since)

	if w := getReceipts(s, h, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := getReceipts(s, h, "", "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := getReceipts(s, h, "?window=year", s.Token()); w.Code != http.StatusBadRequest {
		t.Fatalf("bad window: %d", w.Code)
	}

	w := getReceipts(s, h, "?window=month", s.Token())
	if w.Code != 200 {
		t.Fatalf("month: %d %s", w.Code, w.Body)
	}
	var got receiptsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Window != "month" || got.Receipts.Runs != 2 || got.Receipts.DuplicatesAvoided != 1 || got.Receipts.HeldBack != 1 ||
		got.Receipts.RemoteRuns != 1 || got.Receipts.PoolValues != 1 || got.Receipts.Lanes != 2 {
		t.Fatalf("receipts: %+v", got)
	}
	if want := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC).Add(-30 * 24 * time.Hour); !since.Equal(want) {
		t.Errorf("the ledger was read from %v, want a bounded read from %v", since, want)
	}
	if got.Share.Version != "9.9.9" || !strings.Contains(got.Share.Text, "last 30 days") {
		t.Errorf("share: %+v", got.Share)
	}
	for _, name := range []string{"lane-canary", "cmd-canary", "far-box", "other"} {
		if strings.Contains(w.Body.String(), name) {
			t.Errorf("the response leaks %q: %s", name, w.Body)
		}
	}

	// week is the default.
	var def receiptsResponse
	if err := json.Unmarshal(getReceipts(s, h, "", s.Token()).Body.Bytes(), &def); err != nil || def.Window != "week" {
		t.Errorf("default window: %q, %v", def.Window, err)
	}
}

// The page keeps its budget and its CSP with the panel in it.
func TestPageHasReceiptsPanel(t *testing.T) {
	f := start(t, false)
	code, page := f.do("GET", "/", "", nil)
	if code != 200 {
		t.Fatalf("page: %d", code)
	}
	if len(page) > maxPageBytes {
		t.Fatalf("page is %d bytes, budget %d", len(page), maxPageBytes)
	}
	for _, want := range []string{"function receiptsPanel", `data-rw="`, `data-act="copyshare"`, `api("receipts?window="`, "navigator.clipboard", "hq.rcp"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Count(page, "localStorage.") != 2 {
		t.Error("localStorage access must stay inside the store wrappers")
	}
	if code, _ := f.do("GET", "/api/v1/receipts?window=week", "", nil); code != 200 {
		t.Errorf("receipts through the fixture server: %d", code)
	}
}
