package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func okTag(s string) bool { return s != "" && !strings.ContainsAny(s, " /;$") }

func openTmp(t *testing.T) *Dir {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "q"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSessionRoundTripAtomicAndPrivate(t *testing.T) {
	d, now := openTmp(t), time.Now()
	if err := d.WriteSession("wt-api", "sess-1", now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.Path, "sessions", "wt-api")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode: %v %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(path)); di.Mode().Perm() != 0o700 {
		t.Fatalf("sessions dir mode %v", di.Mode().Perm())
	}
	if got := d.ReadSession("wt-api", now.Add(time.Minute), okTag); got != "sess-1" {
		t.Fatalf("got %q", got)
	}
	if err := d.WriteSession("wt-api", "sess-2", now); err != nil { // overwrite
		t.Fatal(err)
	}
	if got := d.ReadSession("wt-api", now, okTag); got != "sess-2" {
		t.Fatalf("overwrite: got %q", got)
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
}

func TestReadSessionRefusesStaleFutureForeignAndInvalid(t *testing.T) {
	d, now := openTmp(t), time.Now()
	path := filepath.Join(d.Path, "sessions", "lane")
	if got := d.ReadSession("lane", now, okTag); got != "" {
		t.Fatalf("missing file: %q", got)
	}
	cases := []struct {
		name string
		at   time.Time
		id   string
		want string
	}{
		{"fresh", now, "ok", "ok"},
		{"just inside the TTL", now.Add(-SessionTTL + time.Second), "ok", "ok"},
		{"exactly the TTL", now.Add(-SessionTTL), "ok", ""},
		{"older", now.Add(-time.Hour), "ok", ""},
		{"slightly in the future", now.Add(30 * time.Second), "ok", "ok"},
		{"far in the future", now.Add(time.Hour), "ok", ""},
		{"rejected id", now, "has space", ""},
		{"empty id", now, "", ""},
	}
	for _, c := range cases {
		if err := d.WriteSession("lane", c.id, c.at); err != nil {
			t.Fatal(err)
		}
		if got := d.ReadSession("lane", now, okTag); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	for name, body := range map[string]string{"garbage": "not json", "wrong type": `{"session_id":5,"at":"x"}`,
		"big": `{"session_id":"` + strings.Repeat("a", maxSessionBytes) + `"}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := d.ReadSession("lane", now, okTag); got != "" {
			t.Errorf("%s: got %q", name, got)
		}
	}
	// A symlink (even to a valid file) is not followed.
	good := filepath.Join(t.TempDir(), "good")
	if err := os.WriteFile(good, []byte(`{"session_id":"ok","at":"`+now.UTC().Format(time.RFC3339)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.Symlink(good, path); err != nil {
		t.Fatal(err)
	}
	if got := d.ReadSession("lane", now, okTag); got != "" {
		t.Errorf("symlink followed: %q", got)
	}
}

// A lane is a file name: nothing in it may leave the sessions directory.
func TestSessionLaneCannotEscape(t *testing.T) {
	d, now := openTmp(t), time.Now()
	if err := d.WriteSession("../../evil", "ok", now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Path, "..", "evil")); err == nil {
		t.Fatal("wrote outside the sessions directory")
	}
	if got := d.ReadSession("../../evil", now, okTag); got != "ok" {
		t.Fatalf("sanitised lane must round-trip, got %q", got)
	}
}

func TestPruneRemovesOnlyStaleSessionFiles(t *testing.T) {
	d, now := openTmp(t), time.Now()
	for _, l := range []string{"old", "new"} {
		if err := d.WriteSession(l, "ok", now); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(d.Path, "sessions")
	if err := os.WriteFile(filepath.Join(dir, ".tmp-crashed"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-2 * SessionTTL)
	for _, n := range []string{"old", ".tmp-crashed"} {
		if err := os.Chtimes(filepath.Join(dir, n), past, past); err != nil {
			t.Fatal(err)
		}
	}
	d.Prune(func(int, string) bool { return true })
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "new" {
		t.Fatalf("after prune: %v", ents)
	}
	d.Prune(func(int, string) bool { return true }) // no sessions dir problems when repeated
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	d.Prune(func(int, string) bool { return true }) // and none when it is absent
}
