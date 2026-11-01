package cache

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "init"}} {
		if len(args) == 2 && args[0] == "add" {
			if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "sub", "f.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestKeyDependsOnArgvSubdirEnvAndVersion(t *testing.T) {
	repo := gitRepo(t)
	env := map[string]string{"NODE_ENV": "test"}
	getenv := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	base := KeyInput{Dir: repo, Argv: []string{"npm", "test"}, EnvNames: []string{"NODE_ENV", "CI"}, Version: "1.0", Getenv: getenv}
	k0, why := Key(base)
	if why != "" || len(k0) != 64 {
		t.Fatalf("key %q why %q", k0, why)
	}
	if k, _ := Key(base); k != k0 {
		t.Fatal("key must be deterministic")
	}
	variants := []func(*KeyInput){
		func(k *KeyInput) { k.Argv = []string{"npm", "test", "--x"} },
		func(k *KeyInput) { k.Argv = []string{"npm test"} },
		func(k *KeyInput) { k.Dir = filepath.Join(repo, "sub") },
		func(k *KeyInput) { k.Version = "1.1" },
		func(k *KeyInput) { k.BaseRef = "origin/main" },
		func(k *KeyInput) { k.EnvNames = []string{"NODE_ENV"} },
	}
	for i, mod := range variants {
		in := base
		mod(&in)
		if k, _ := Key(in); k == k0 || k == "" {
			t.Errorf("variant %d must change the key", i)
		}
	}
	env["CI"] = "" // set-but-empty differs from unset
	if k, _ := Key(base); k == k0 {
		t.Error("CI set to empty must change the key")
	}
	if _, why := Key(KeyInput{Dir: t.TempDir(), Getenv: getenv}); why != "not in a git repository" {
		t.Errorf("why = %q", why)
	}
}

func newTestStore(t *testing.T, maxBytes, maxEntry int64) *Store {
	return &Store{Dir: filepath.Join(t.TempDir(), "results"), TTL: time.Hour, MaxBytes: maxBytes, MaxEntryBytes: maxEntry}
}

func key(c byte) string { return strings.Repeat(string(c), 64) }

func commit(t *testing.T, s *Store, k string, out string, created time.Time) {
	t.Helper()
	var sink bytes.Buffer
	r, err := s.NewRecorder(&sink, &sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stdout().Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(r, Meta{Key: k, Created: created, Machine: "local"}); err != nil {
		t.Fatal(err)
	}
	if sink.String() != out {
		t.Fatal("recorder must pass output through")
	}
}

func TestRecorderReplayKeepsStreamsAndOrder(t *testing.T) {
	s := newTestStore(t, 1<<20, 1<<20)
	var live bytes.Buffer
	r, err := s.NewRecorder(&live, &live)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct {
		err bool
		s   string
	}{{false, "a"}, {true, "B"}, {false, "c"}, {true, "D"}} {
		dst := r.Stdout()
		if w.err {
			dst = r.Stderr()
		}
		_, _ = dst.Write([]byte(w.s))
	}
	if err := s.Commit(r, Meta{Key: key('a'), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var out, errb, both bytes.Buffer
	m := s.Get(key('a'), time.Now())
	if m == nil || m.Size != 4*6 {
		t.Fatalf("meta %+v", m)
	}
	if _, err := s.Replay(m, &out, &errb); err != nil || out.String() != "ac" || errb.String() != "BD" {
		t.Fatalf("%v %q %q", err, out.String(), errb.String())
	}
	if _, err := s.Replay(m, &both, &both); err != nil || both.String() != "aBcD" {
		t.Fatalf("order: %q", both.String())
	}
}

func TestRecorderOverEntryCapIsNotStored(t *testing.T) {
	s := newTestStore(t, 1<<20, 100)
	var live bytes.Buffer
	r, _ := s.NewRecorder(&live, &live)
	_, _ = r.Stdout().Write(bytes.Repeat([]byte("x"), 200))
	if err := s.Commit(r, Meta{Key: key('b'), Created: time.Now()}); err == nil || !strings.Contains(err.Error(), "max_entry_mb") {
		t.Fatalf("err = %v", err)
	}
	if live.Len() != 200 || s.Get(key('b'), time.Now()) != nil {
		t.Fatal("output must pass through, and nothing be stored")
	}
	if ents, _ := os.ReadDir(s.Dir); len(ents) != 0 { // the temp recording is gone
		t.Fatalf("leftovers: %v", ents)
	}
}

func TestEvictionByTTLAndSize(t *testing.T) {
	s := newTestStore(t, 300, 300)
	now := time.Now()
	commit(t, s, key('1'), strings.Repeat("1", 95), now.Add(-3*time.Minute)) // 100 bytes stored
	commit(t, s, key('2'), strings.Repeat("2", 95), now.Add(-2*time.Minute))
	commit(t, s, key('3'), strings.Repeat("3", 95), now.Add(-time.Minute))
	if l, _ := s.List(); len(l) != 3 {
		t.Fatalf("3 entries fit 300 bytes, got %d", len(l))
	}
	commit(t, s, key('4'), strings.Repeat("4", 95), now)
	l, _ := s.List()
	if len(l) != 3 || s.Get(key('1'), now) != nil || s.Get(key('4'), now) == nil {
		t.Fatalf("the oldest must go first: %+v", l)
	}
	if s.Get(key('2'), now.Add(time.Hour)) != nil {
		t.Fatal("Get must ignore expired entries")
	}
	if err := s.Evict(now.Add(time.Hour - 30*time.Second)); err != nil { // 2 and 3 are past the TTL, 4 is not
		t.Fatal(err)
	}
	if l, _ := s.List(); len(l) != 1 || l[0].Key != key('4') {
		t.Fatalf("expired entries must be evicted: %+v", l)
	}
	if n, err := s.Clear(); err != nil || n != 1 {
		t.Fatalf("clear %d %v", n, err)
	}
}
