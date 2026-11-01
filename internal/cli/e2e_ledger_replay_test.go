package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// A coalesced result proves the tree the cache key was computed on. If the
// work tree is edited while the follower waits, the ledger must not stamp the
// edited tree on a result that was computed on the clean one.
//
// The test plays the leader itself (holds the in-flight lock, keeps the stored
// entry aside), so it controls exactly when the result appears.
func TestE2ELedgerCoalescedReplayDoesNotClaimAnEditedTree(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")
	argv := cachedCmd(h, "rp", 0)

	// A real run stores the entry; its key and files are then ours to move.
	if rc, out := h.run(cacheArgs([]string{"--cache"}, argv)...); rc != 0 {
		t.Fatalf("warm-up rc=%d\n%s", rc, out)
	}
	results := filepath.Join(h.root, "cache", "heavyq", "results")
	metas, _ := filepath.Glob(filepath.Join(results, "*.json"))
	if len(metas) != 1 {
		t.Fatalf("want one stored entry, got %v", metas)
	}
	key := strings.TrimSuffix(filepath.Base(metas[0]), ".json")
	aside := t.TempDir()
	flight := &cache.Flight{Dir: filepath.Join(h.root, "q", "inflight")}
	start, err := procinfo.Default.StartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	// follow runs one follower that waits for our fake leader; edit runs while
	// it waits; then the result "appears" and the leader ends.
	follow := func(session string, edit func()) history.Record {
		t.Helper()
		for _, ext := range []string{".json", ".out"} {
			if err := os.Rename(filepath.Join(results, key+ext), filepath.Join(aside, key+ext)); err != nil {
				t.Fatal(err)
			}
		}
		lead, err := flight.TryLead(cache.Party{Key: key, Pid: os.Getpid(), Start: start, Lane: "fake", Cmd: "x", Since: time.Now()})
		if err != nil || lead == nil {
			t.Fatalf("could not take the in-flight lock: %v", err)
		}
		p := h.start(cacheArgs([]string{"--cache", "--session", session}, argv)...)
		h.until("the follower waits", func() bool { return strings.Contains(p.out.String(), "an identical run is in progress") })
		edit()
		for _, ext := range []string{".json", ".out"} {
			if err := os.Rename(filepath.Join(aside, key+ext), filepath.Join(results, key+ext)); err != nil {
				t.Fatal(err)
			}
		}
		flight.EndLead(key, lead)
		if rc := p.wait(t, 30*time.Second); rc != 0 {
			t.Fatalf("follower rc=%d\n%s", rc, p.out)
		}
		r := h.lastRecord()
		if r.Cache != "coalesced" || r.Session != session {
			t.Fatalf("not the follower's coalesced record: %+v\n%s", r, p.out)
		}
		return r
	}
	marker := filepath.Join(repo, "marker.txt")

	// Control: nothing changed while it waited, so the proof is the key's tree.
	if r := follow("calm", func() {}); r.Tree != tree || r.Dirty || r.Commit != head {
		t.Fatalf("unchanged tree: %+v, want tree %s clean on %s", r, tree, head)
	}
	// The tree is edited while the follower waits: the result still came from
	// the clean tree, so no tree is claimed, and dirty says what the tree is now.
	edited := follow("busy", func() {
		if err := os.WriteFile(marker, []byte("edited while waiting\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if edited.Tree != "" || !edited.Dirty || edited.Commit != head {
		t.Fatalf("edited during the wait: %+v, want no tree, dirty, commit %s", edited, head)
	}
}
