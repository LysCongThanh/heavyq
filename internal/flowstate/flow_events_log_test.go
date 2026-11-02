package flowstate

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

func TestEventsRoundTripInOrderAndSkipTheOld(t *testing.T) {
	common := t.TempDir()
	for i, e := range []flow.Event{
		{At: base.Add(2 * time.Minute), Kind: flow.EventCleared, Lane: "a"},
		{At: base, Kind: flow.EventBlocked, Lane: "a", Class: flow.ClassPermissionRequest, Reason: "permission_request: Bash(ls)"},
		{At: base.Add(-time.Hour), Kind: flow.EventBlocked, Lane: "old"},
	} {
		if err := AppendEvent(common, e); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	got, err := ReadEvents(common, base.Add(-time.Minute))
	if err != nil || len(got) != 2 || got[0].Kind != flow.EventBlocked || got[1].Kind != flow.EventCleared {
		t.Fatalf("oldest first, without the old one: %+v %v", got, err)
	}
}

func TestEventsRefuseWhatCannotBeRead(t *testing.T) {
	if err := AppendEvent(t.TempDir(), flow.Event{Kind: flow.EventBlocked}); err == nil {
		t.Error("an event without a lane is refused")
	}
	if err := AppendEvent(t.TempDir(), flow.Event{Lane: "a"}); err == nil {
		t.Error("an event without a kind is refused")
	}
}

func TestReadEventsSkipsTornAndForeignLines(t *testing.T) {
	common := t.TempDir()
	_ = AppendEvent(common, flow.Event{At: base, Kind: flow.EventBlocked, Lane: "a"})
	f, _ := os.OpenFile(eventsPath(common), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("{not json\n{\"kind\":\"blocked\"}\n[1]\n")
	_ = f.Close()
	got, err := ReadEvents(common, time.Time{})
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestConcurrentAppendsNeverInterleave(t *testing.T) {
	common := t.TempDir()
	var wg sync.WaitGroup
	const writers, each = 8, 25
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := AppendEvent(common, flow.Event{At: base, Kind: flow.EventBlocked, Lane: "a", Reason: strings.Repeat("x", 300)}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := ReadEvents(common, time.Time{})
	if err != nil || len(got) != writers*each {
		t.Fatalf("%d events, want %d (%v)", len(got), writers*each, err)
	}
}

func TestEventLogRotatesAndKeepsOneOlderGeneration(t *testing.T) {
	common := t.TempDir()
	pad := strings.Repeat("y", 4000)
	for i := 0; i < 160; i++ { // about 640 KB: past eventsRotateAt
		if err := AppendEvent(common, flow.Event{At: base.Add(time.Duration(i) * time.Second), Kind: flow.EventBlocked, Lane: "a", Reason: pad}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(eventsPath(common) + ".1"); err != nil {
		t.Fatalf("no rotated generation: %v", err)
	}
	got, err := ReadEvents(common, time.Time{})
	if err != nil || len(got) != 160 {
		t.Fatalf("rotation must not lose the recent past: %d events (%v)", len(got), err)
	}
}

func TestBlockMarkIsTakenOnceAndNeedsAValidSession(t *testing.T) {
	t.Setenv("HEAVYQ_HISTORY", t.TempDir()+"/history.jsonl")
	if err := MarkBlocked("sess-1", "/r/.git", "docs"); err != nil {
		t.Fatal(err)
	}
	if c, l, ok := TakeBlockMark("sess-1"); !ok || c != "/r/.git" || l != "docs" {
		t.Fatalf("%q %q %v", c, l, ok)
	}
	if _, _, ok := TakeBlockMark("sess-1"); ok {
		t.Error("a mark is taken once")
	}
	if _, _, ok := TakeBlockMark("never-blocked"); ok {
		t.Error("the common case: nothing blocked")
	}
	if err := MarkBlocked("../escape", "/r/.git", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := TakeBlockMark("../escape"); ok {
		t.Error("an unsafe session id never becomes a path")
	}
}
