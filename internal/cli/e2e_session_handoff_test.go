package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// sessionDir opens the rig's queue directory, where the hook leaves sessions.
func sessionDir(t *testing.T, h *rig) *queue.Dir {
	t.Helper()
	q, err := queue.Open(filepath.Join(h.root, "q"))
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// The hook leaves the Claude session in <queue dir>/sessions/<lane>; a run on
// that lane picks it up, unless the caller said otherwise or the file is old.
func TestE2ESessionHandoffFromTheHooksFile(t *testing.T) {
	h := newRig(t, baseConfig)
	q := sessionDir(t, h)
	runOn := func(lane string, extra ...string) string {
		t.Helper()
		args := append(append([]string{"4", "--lane", lane}, extra...), "--", "true")
		if rc, out := h.run(args...); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		return h.lastRecord().Session
	}
	put := func(lane, id string, age time.Duration) {
		t.Helper()
		if err := q.WriteSession(lane, id, time.Now().Add(-age)); err != nil {
			t.Fatal(err)
		}
	}

	put("api", "claude-A", time.Minute)
	if got := runOn("api"); got != "claude-A" {
		t.Fatalf("session from the file: %q", got)
	}
	if got := runOn("web"); got != "" {
		t.Fatalf("another lane must not inherit it: %q", got)
	}
	if got := runOn("api", "--session", "explicit"); got != "explicit" {
		t.Fatalf("--session must win over the file: %q", got)
	}
	h.env = append(h.env, "HEAVYQ_SESSION=from-env")
	if got := runOn("api"); got != "from-env" {
		t.Fatalf("$HEAVYQ_SESSION must win over the file: %q", got)
	}
	h.env = h.env[:len(h.env)-1]

	put("api", "claude-B", 11*time.Minute) // past the 10 minute TTL
	if got := runOn("api"); got != "" {
		t.Fatalf("a stale file was used: %q", got)
	}
	put("api", "bad id;rm", time.Minute) // fails the same validation as --session
	if got := runOn("api"); got != "" {
		t.Fatalf("an invalid id was used: %q", got)
	}
	path := filepath.Join(h.root, "q", "sessions", "api")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runOn("api"); got != "" {
		t.Fatalf("a corrupt file was used: %q", got)
	}
}

// Stale session files are removed together with dead tickets.
func TestE2ESessionFilesArePrunedWithTickets(t *testing.T) {
	h := newRig(t, baseConfig)
	q := sessionDir(t, h)
	for _, lane := range []string{"old", "fresh"} {
		if err := q.WriteSession(lane, "s", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-queue.SessionTTL - time.Minute)
	if err := os.Chtimes(filepath.Join(h.root, "q", "sessions", "old"), past, past); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("status"); rc != 0 {
		t.Fatalf("status rc=%d\n%s", rc, out)
	}
	if _, err := os.Stat(filepath.Join(h.root, "q", "sessions", "old")); !os.IsNotExist(err) {
		t.Fatalf("the stale session file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "q", "sessions", "fresh")); err != nil {
		t.Fatalf("a fresh session file was pruned: %v", err)
	}
}
