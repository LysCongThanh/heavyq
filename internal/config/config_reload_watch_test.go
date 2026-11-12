package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rewriteConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// settled is the check after the one that first read an edit: the first only
// remembers what it read (the writer may not be done), so it returns nothing.
func settled(t *testing.T, w *Watch, cur *Config) (*Config, error) {
	t.Helper()
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("the first read of an edit is not used yet: %+v %v", fresh, err)
	}
	return w.Changed(cur)
}

// An editor that writes the file in place can be read half-way: a check never
// uses what it read once. A content is used when the next check reads it the
// same, so a half-written file that the writer then completes is never used.
func TestWatchUsesAnEditOnlyWhenTwoChecksReadItTheSame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	rewriteConfig(t, path, "[local]\nheavy_slots = 1\nlegacy_lock_files = [\"/tmp/a.lock\"]\n")
	cur, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatch(cur)
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("untouched: %v %v", fresh, err)
	}
	rewriteConfig(t, path, "[local]\nheavy_slots = 1\n") // the slot list not written yet
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("a content read once is not used: %+v %v", fresh, err)
	}
	rewriteConfig(t, path, "[local]\nheavy_slots = 1\nlegacy_lock_files = [\"/tmp/a.lock\", \"/tmp/b.lock\"]\n")
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("the completed file is read once here, so not used yet: %+v %v", fresh, err)
	}
	fresh, err := w.Changed(cur)
	if err != nil || fresh == nil || len(fresh.Local.LegacyLockFiles) != 2 {
		t.Fatalf("read the same twice, the edit is used: %+v %v", fresh, err)
	}
}

// A watch is quiet while the file is untouched, returns an edit (once it reads
// the same twice), reports an edit that does not load once (the caller keeps
// its config), and picks up the next good edit.
func TestWatchFollowsEditsAndKeepsTheLastGoodConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	rewriteConfig(t, path, "[local]\nheavy_slots = 1\n")
	cur, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Origin.Path != path || !cur.Origin.Required || cur.Origin.Sum == "" || cur.Origin.Sum == noFileSum {
		t.Fatalf("origin = %+v", cur.Origin)
	}
	w := NewWatch(cur)
	for range 2 {
		if fresh, err := w.Changed(cur); fresh != nil || err != nil {
			t.Fatalf("untouched file: %v %v", fresh, err)
		}
	}

	rewriteConfig(t, path, "[local]\nheavy_slots = 3\n")
	fresh, err := settled(t, w, cur)
	if err != nil || fresh == nil || fresh.Local.HeavySlots != 3 || fresh.Origin.Sum == cur.Origin.Sum {
		t.Fatalf("edit: %+v %v", fresh, err)
	}
	cur = fresh

	rewriteConfig(t, path, "[local]\nheavy_slots = 3\nmax_load_ratio = 100\n")
	if fresh, err := settled(t, w, cur); fresh != nil || err == nil || !strings.Contains(err.Error(), "max_load_ratio") {
		t.Fatalf("an invalid edit is refused: %v %v", fresh, err)
	}
	rewriteConfig(t, path, "[local]\nheavy_slots = 3\nmax_load_ratio = 100 \n") // same refusal, other bytes
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("the same refusal is reported once: %v %v", fresh, err)
	}

	rewriteConfig(t, path, "[local]\nheavy_slots = 4\n")
	if fresh, err := settled(t, w, cur); err != nil || fresh == nil || fresh.Local.HeavySlots != 4 {
		t.Fatalf("the next good edit: %+v %v", fresh, err)
	}
}

// A file the config was read from that disappears is refused once, required or
// not: the waiter keeps its last good config rather than falling back to the
// defaults. When the file comes back, its content is picked up (or, when it is
// the same, nothing changes).
func TestWatchOnAMissingFile(t *testing.T) {
	dir := t.TempDir()
	for _, required := range []bool{true, false} {
		path := filepath.Join(dir, "c.toml")
		rewriteConfig(t, path, "[local]\nheavy_slots = 1\n")
		cur, err := Load(path, required)
		if err != nil {
			t.Fatal(err)
		}
		w := NewWatch(cur)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if fresh, err := settled(t, w, cur); fresh != nil || err == nil || !strings.Contains(err.Error(), "is gone") {
			t.Fatalf("required=%v: %v %v", required, fresh, err)
		}
		if fresh, err := w.Changed(cur); fresh != nil || err != nil {
			t.Fatalf("required=%v: the missing file is reported once: %v %v", required, fresh, err)
		}
		rewriteConfig(t, path, "[local]\nheavy_slots = 1\n")
		if fresh, err := w.Changed(cur); fresh != nil || err != nil {
			t.Fatalf("required=%v: the same file back changes nothing: %v %v", required, fresh, err)
		}
		rewriteConfig(t, path, "[local]\nheavy_slots = 5\n")
		if fresh, err := settled(t, w, cur); err != nil || fresh == nil || fresh.Local.HeavySlots != 5 {
			t.Fatalf("required=%v: an edit after the file came back: %+v %v", required, fresh, err)
		}
	}
	// A config that never came from a file (defaults) has nothing to lose, and
	// a file created later is picked up.
	path := filepath.Join(dir, "later.toml")
	cur, err := Load(path, false)
	if err != nil || cur.Origin.Sum != noFileSum {
		t.Fatalf("defaults: %+v %v", cur, err)
	}
	w := NewWatch(cur)
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("still no file: %v %v", fresh, err)
	}
	rewriteConfig(t, path, "[local]\nheavy_slots = 7\n")
	if fresh, err := settled(t, w, cur); err != nil || fresh == nil || fresh.Local.HeavySlots != 7 {
		t.Fatalf("a file created later: %+v %v", fresh, err)
	}
	if cfg, err := Load("", false); err != nil || cfg.Origin.Sum != noFileSum || NewWatch(cfg) == nil {
		t.Fatalf("defaults: %+v %v", cfg, err)
	} else if fresh, err := NewWatch(cfg).Changed(cfg); fresh != nil || err != nil {
		t.Fatalf("nothing to watch: %v %v", fresh, err)
	}
}
