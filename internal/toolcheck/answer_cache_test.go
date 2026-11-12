package toolcheck

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCacheServesFreshAnswersAndExpiresThem(t *testing.T) {
	c := Cache{Dir: filepath.Join(t.TempDir(), "remote-tools")}
	key := Key("~/.local/bin", []string{"ssh", "box"})
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if err := c.Store("box", key, Answers(map[string]string{"go": "/usr/bin/go", "npx": ""}, t0)); err != nil {
		t.Fatal(err)
	}
	known, stale := c.Lookup("box", key, []string{"go", "npx", "pnpm"}, t0.Add(time.Minute))
	if !known["go"].Found || known["go"].Path != "/usr/bin/go" || known["npx"].Found || !reflect.DeepEqual(stale, []string{"pnpm"}) {
		t.Fatalf("a minute later: known=%+v stale=%q", known, stale)
	}
	// A "missing" answer is kept for MissingTTL only: the tool may be installed meanwhile.
	known, stale = c.Lookup("box", key, []string{"go", "npx"}, t0.Add(MissingTTL+time.Second))
	if _, ok := known["npx"]; ok || !reflect.DeepEqual(stale, []string{"npx"}) || !known["go"].Found {
		t.Fatalf("after MissingTTL: known=%+v stale=%q", known, stale)
	}
	if _, stale = c.Lookup("box", key, []string{"go"}, t0.Add(FoundTTL+time.Second)); !reflect.DeepEqual(stale, []string{"go"}) {
		t.Fatalf("after FoundTTL: stale=%q", stale)
	}
	// Another env_path or transport is another PATH: nothing cached for the old one counts.
	if _, stale = c.Lookup("box", Key("", []string{"ssh", "box"}), []string{"go"}, t0); !reflect.DeepEqual(stale, []string{"go"}) {
		t.Fatalf("other key: stale=%q", stale)
	}
	// Storing merges with what is there.
	if err := c.Store("box", key, Answers(map[string]string{"pnpm": "/opt/pnpm"}, t0)); err != nil {
		t.Fatal(err)
	}
	if known, _ = c.Lookup("box", key, []string{"go", "pnpm"}, t0); !known["go"].Found || !known["pnpm"].Found {
		t.Fatalf("merge lost an answer: %+v", known)
	}
	if err := c.Forget("box"); err != nil {
		t.Fatal(err)
	}
	if _, stale = c.Lookup("box", key, []string{"go"}, t0); !reflect.DeepEqual(stale, []string{"go"}) {
		t.Fatalf("after Forget: stale=%q", stale)
	}
	if err := c.Forget("box"); err != nil {
		t.Fatalf("forgetting nothing is not an error: %v", err)
	}
}

func TestCacheFailsOpenOnABrokenFileAndRefusesUnsafeNames(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(c.Dir, "box.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stale := c.Lookup("box", "k", []string{"go"}, time.Now()); !reflect.DeepEqual(stale, []string{"go"}) {
		t.Fatalf("a broken file is a cache miss: stale=%q", stale)
	}
	if err := c.Store("../escape", "k", Answers(map[string]string{"go": "/x"}, time.Now())); err == nil {
		t.Fatal("a remote name that is not a safe file name must be refused")
	}
	none := Cache{}
	if err := none.Store("box", "k", Answers(map[string]string{"go": "/x"}, time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, stale := none.Lookup("box", "k", []string{"go"}, time.Now()); len(stale) != 1 {
		t.Fatal("without a directory nothing is cached")
	}
}
