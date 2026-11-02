package flowstate

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

func TestQuotaMarkIsKeptThroughTheGraceAndTakenOnce(t *testing.T) {
	t.Setenv("HEAVYQ_HISTORY", t.TempDir()+"/history.jsonl")
	t0 := time.Date(2026, 11, 2, 19, 20, 0, 0, time.UTC)
	if got := TakeQuotaMark(t0, time.Minute); got != nil {
		t.Fatalf("no mark, nothing: %v", got)
	}
	if err := MarkQuota("/a/.git", t0); err != nil {
		t.Fatal(err)
	}
	if err := MarkQuota("/b/.git", t0.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := MarkQuota("/a/.git", t0.Add(time.Minute)); err != nil { // the same repository again: listed once
		t.Fatal(err)
	}
	if got := TakeQuotaMark(t0.Add(90*time.Second), 2*time.Minute); got != nil {
		t.Fatalf("inside the grace of the latest stop the mark stays: %v", got)
	}
	got := TakeQuotaMark(t0.Add(4*time.Minute), 2*time.Minute)
	if !reflect.DeepEqual(got, []string{"/a/.git", "/b/.git"}) {
		t.Fatalf("both repositories, once: %v", got)
	}
	if got := TakeQuotaMark(t0.Add(5*time.Minute), 0); got != nil {
		t.Fatalf("taken: %v", got)
	}
}

func TestQuotaMarkDamagedIsForgotten(t *testing.T) {
	t.Setenv("HEAVYQ_HISTORY", t.TempDir()+"/history.jsonl")
	if err := MarkQuota("/a/.git", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(quotaMarkPath(), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := TakeQuotaMark(time.Now().Add(time.Hour), 0); got != nil {
		t.Fatalf("%v", got)
	}
	if _, err := os.Stat(quotaMarkPath()); err == nil {
		t.Error("a damaged mark must not stay to be misread every tool call")
	}
}

// quota_cleared is the one fleet-level event: it needs no lane. Every other kind still does.
func TestQuotaClearedEventNeedsNoLane(t *testing.T) {
	common := t.TempDir()
	at := time.Date(2026, 11, 2, 21, 20, 0, 0, time.UTC)
	if err := AppendEvent(common, flow.Event{At: at, Kind: flow.EventQuotaCleared, Session: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(common, flow.Event{At: at, Kind: flow.EventBlocked}); err == nil {
		t.Error("a blocked event without a lane must be refused")
	}
	evs, err := ReadEvents(common, at.Add(-time.Hour))
	if err != nil || len(evs) != 1 || evs[0].Kind != flow.EventQuotaCleared || evs[0].Lane != "" {
		t.Fatalf("%+v %v", evs, err)
	}
}
