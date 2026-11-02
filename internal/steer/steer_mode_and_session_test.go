package steer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseModeAndEffective(t *testing.T) {
	for _, s := range []string{"off", "guard", "destroy", " guard\n"} {
		if _, err := ParseMode(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"", "GUARD", "on", "strict", "guard,destroy"} {
		if _, err := ParseMode(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	cases := []struct {
		env    string
		stored Mode
		want   Mode
		src    string
	}{
		{"", Guard, Guard, "stored"},
		{"  ", Destroy, Destroy, "stored"},
		{"off", Destroy, Off, EnvVar},
		{"destroy", Off, Destroy, EnvVar},
		{"banana", Destroy, Off, "treated as off"}, // a kill switch must never fail to kill
	}
	for _, c := range cases {
		got, src := Effective(c.env, c.stored)
		if got != c.want || !strings.Contains(src, c.src) {
			t.Errorf("Effective(%q, %s) = %s, %q; want %s, %q", c.env, c.stored, got, src, c.want, c.src)
		}
	}
}

func TestStoredModeRoundTripsAndDamageIsOff(t *testing.T) {
	common := t.TempDir()
	if got := ReadStored(common); got != Off {
		t.Fatalf("default = %s, want off (opt-in)", got)
	}
	for _, m := range []Mode{Guard, Destroy, Off} {
		if err := WriteStored(common, m); err != nil {
			t.Fatal(err)
		}
		if got := ReadStored(common); got != m {
			t.Fatalf("stored %s, read %s", m, got)
		}
	}
	if err := WriteStored(common, Mode("maybe")); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if fi, err := os.Stat(ModeFile(common)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode file: %v %v", fi, err)
	}
	for _, junk := range []string{"guard destroy", "\x00\x01", strings.Repeat("guard", 100)} {
		if err := os.WriteFile(ModeFile(common), []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := ReadStored(common); got != Off {
			t.Errorf("damaged file %q read as %s, want off", junk, got)
		}
	}
}

func TestSessionStateSaveLoadAndBaseline(t *testing.T) {
	s := OpenSessions(t.TempDir())
	if _, ok := s.Load("abc"); ok {
		t.Fatal("no record yet")
	}
	st := SessionState{Started: time.Now(), HasBaseline: true, Baseline: []string{"go test ./...", "lint"}, Blocks: 2, LastSig: "t#a"}
	if err := s.Save("abc", st); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Load("abc")
	if !ok || got.Blocks != 2 || got.LastSig != "t#a" || !got.IsBaseline("lint") || got.IsBaseline("other") {
		t.Fatalf("%+v %v", got, ok)
	}
	if (SessionState{Baseline: []string{"x"}}).IsBaseline("x") {
		t.Error("a list without HasBaseline is not a baseline")
	}
	for _, bad := range []string{"", "../x", "a/b", strings.Repeat("a", 81), "sp ace"} {
		if ValidSessionID(bad) {
			t.Errorf("%q accepted as a session id", bad)
		}
		if err := s.Save(bad, st); err == nil {
			t.Errorf("Save(%q) must refuse", bad)
		}
	}
}

func TestSessionStateDamagedOrForeignIsNoMemory(t *testing.T) {
	common := t.TempDir()
	s := OpenSessions(common)
	if err := s.Save("abc", SessionState{Blocks: 1}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(common, "heavyq", "steer-sessions", "abc.json")
	for _, junk := range []string{"{not json", `{"v":99,"blocks":3}`, strings.Repeat("x", 70<<10)} {
		if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.Load("abc"); ok {
			t.Errorf("damaged record %.20q must read as no memory", junk)
		}
	}
}

func TestSessionsPruneAndLatestCanary(t *testing.T) {
	common := t.TempDir()
	s := OpenSessions(common)
	now := time.Now()
	for id, at := range map[string]time.Time{"old": now.Add(-time.Hour), "new": now} {
		if err := s.Save(id, SessionState{Canary: "hq-" + id, CanaryAt: at, Transcript: "/t/" + id + ".jsonl"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save("nocanary", SessionState{}); err != nil {
		t.Fatal(err)
	}
	id, st, ok := s.LatestCanary()
	if !ok || id != "new" || st.Canary != "hq-new" {
		t.Fatalf("latest canary = %s %+v %v", id, st, ok)
	}
	past := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(common, "heavyq", "steer-sessions", "old.json"), past, past); err != nil {
		t.Fatal(err)
	}
	s.Prune(14*24*time.Hour, now)
	if _, ok := s.Load("old"); ok {
		t.Error("a record untouched for 30 days must be pruned")
	}
	if _, ok := s.Load("new"); !ok {
		t.Error("a fresh record must stay")
	}
	if _, _, ok := OpenSessions(t.TempDir()).LatestCanary(); ok {
		t.Error("an empty store has no canary")
	}
}
