package runspan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)

func plan(goal string, hours int) Span {
	end := t0.Add(time.Duration(hours) * time.Hour)
	return Span{Goal: goal, Items: []string{"a", "b"}, Parallel: 3, Host: "h", ExpectedEnd: &end}
}

func TestOpenWritesTheDocumentedFile(t *testing.T) {
	common := t.TempDir()
	s, err := Open(common, plan("ship", 8), t0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(common, "heavyq", "runs", s.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "started", "expected_end", "goal", "items", "parallel", "host"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("key %q missing in %s", k, raw)
		}
	}
	if _, ok := doc["ended"]; ok {
		t.Errorf("an open span must not carry ended: %s", raw)
	}
	if info, _ := os.Stat(filepath.Join(common, "heavyq", "runs", s.ID+".json")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
}

func TestExpectedEndIsNullWithoutHours(t *testing.T) {
	common := t.TempDir()
	s, err := Open(common, Span{Goal: "g"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(spanPath(common, s.ID))
	if !strings.Contains(string(raw), `"expected_end":null`) || !strings.Contains(string(raw), `"items":[]`) {
		t.Errorf("want null expected_end and [] items: %s", raw)
	}
}

func TestCurrentEndAndList(t *testing.T) {
	common := t.TempDir()
	if cur, err := Current(common, t0); err != nil || cur != nil {
		t.Fatalf("empty repo: %v %v", cur, err)
	}
	a, err := Open(common, plan("first", 2), t0)
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := Current(common, t0.Add(time.Minute))
	if cur == nil || cur.ID != a.ID {
		t.Fatalf("current = %v, want %s", cur, a.ID)
	}
	// A second span ends the first: one run at a time.
	b, err := Open(common, plan("second", 2), t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(common, a.ID); got.Ended == nil {
		t.Error("opening a span must end the one still open")
	}
	if cur, _ := Current(common, t0.Add(time.Hour)); cur == nil || cur.ID != b.ID {
		t.Fatalf("current = %v, want %s", cur, b.ID)
	}
	ended, err := End(common, "", t0.Add(90*time.Minute))
	if err != nil || ended.ID != b.ID || ended.Ended == nil {
		t.Fatalf("End current: %+v %v", ended, err)
	}
	if _, err := End(common, b.ID, t0); !errors.Is(err, ErrEnded) {
		t.Errorf("ending twice: %v", err)
	}
	if _, err := End(common, "", t0); !errors.Is(err, ErrNoneOpen) {
		t.Errorf("nothing open: %v", err)
	}
	all, err := List(common)
	if err != nil || len(all) != 2 || all[0].ID != b.ID {
		t.Fatalf("list newest first: %+v %v", all, err)
	}
}

func TestLapsedSpanIsNotCurrent(t *testing.T) {
	common := t.TempDir()
	if _, err := Open(common, plan("night", 8), t0); err != nil {
		t.Fatal(err)
	}
	if cur, _ := Current(common, t0.Add(8*time.Hour+30*time.Minute)); cur == nil {
		t.Error("within the grace after the expected end the span is still current")
	}
	if cur, _ := Current(common, t0.Add(10*time.Hour)); cur != nil {
		t.Errorf("a span 2h past its end is lapsed: %+v", cur)
	}
	if _, err := Open(common, Span{Goal: "no end"}, t0); err != nil {
		t.Fatal(err)
	}
	if cur, _ := Current(common, t0.Add(25*time.Hour)); cur != nil {
		t.Errorf("a span without an end lapses after 24h: %+v", cur)
	}
}

func TestBadIDsAndDamagedFiles(t *testing.T) {
	common := t.TempDir()
	for _, id := range []string{"../x", "a/b", ".hidden", "", strings.Repeat("a", 65)} {
		if _, err := Get(common, id); !errors.Is(err, ErrBadID) {
			t.Errorf("Get(%q) = %v, want ErrBadID", id, err)
		}
	}
	if _, err := Open(common, Span{ID: "../evil"}, t0); !errors.Is(err, ErrBadID) {
		t.Errorf("Open with a bad id: %v", err)
	}
	if _, err := Get(common, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v", err)
	}
	a, _ := Open(common, plan("ok", 1), t0)
	if _, err := Open(common, Span{ID: a.ID}, t0); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate id: %v", err)
	}
	dir := Dir(common)
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "liar.json"), []byte(`{"id":"other"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "huge.json"), []byte(`{"id":"huge","goal":"`+strings.Repeat("x", maxSpanBytes)+`"}`), 0o600)
	all, err := List(common)
	if err != nil || len(all) != 1 || all[0].ID != a.ID {
		t.Fatalf("damaged files must be skipped: %+v %v", all, err)
	}
}
