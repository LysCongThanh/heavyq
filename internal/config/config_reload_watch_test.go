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

// A watch is quiet while the file is untouched, returns an edit once, reports
// an edit that does not load once (the caller keeps its config), and picks up
// the next good edit.
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
	fresh, err := w.Changed(cur)
	if err != nil || fresh == nil || fresh.Local.HeavySlots != 3 || fresh.Origin.Sum == cur.Origin.Sum {
		t.Fatalf("edit: %+v %v", fresh, err)
	}
	cur = fresh

	rewriteConfig(t, path, "[local]\nheavy_slots = 3\nmax_load_ratio = 100\n")
	if fresh, err := w.Changed(cur); fresh != nil || err == nil || !strings.Contains(err.Error(), "max_load_ratio") {
		t.Fatalf("an invalid edit is refused: %v %v", fresh, err)
	}
	rewriteConfig(t, path, "[local]\nheavy_slots = 3\nmax_load_ratio = 100 \n") // same refusal, other bytes
	if fresh, err := w.Changed(cur); fresh != nil || err != nil {
		t.Fatalf("the same refusal is reported once: %v %v", fresh, err)
	}

	rewriteConfig(t, path, "[local]\nheavy_slots = 4\n")
	if fresh, err := w.Changed(cur); err != nil || fresh == nil || fresh.Local.HeavySlots != 4 {
		t.Fatalf("the next good edit: %+v %v", fresh, err)
	}
}

// A required file that disappears is refused; an optional one falls back to
// the defaults, as Load at start would.
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
		fresh, err := w.Changed(cur)
		if required && (fresh != nil || err == nil) {
			t.Fatalf("required: %v %v", fresh, err)
		}
		if !required && (err != nil || fresh == nil || fresh.Origin.Sum != noFileSum || fresh.Local.HeavySlots != 2) {
			t.Fatalf("optional: %+v %v", fresh, err)
		}
	}
	if cfg, err := Load("", false); err != nil || cfg.Origin.Sum != noFileSum || NewWatch(cfg) == nil {
		t.Fatalf("defaults: %+v %v", cfg, err)
	} else if fresh, err := NewWatch(cfg).Changed(cfg); fresh != nil || err != nil {
		t.Fatalf("nothing to watch: %v %v", fresh, err)
	}
}
