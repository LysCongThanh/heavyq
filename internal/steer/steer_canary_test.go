package steer

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNewCanaryIsShortAndRandom(t *testing.T) {
	re := regexp.MustCompile(`^hq-[0-9a-f]{8}$`)
	a, err := NewCanary()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewCanary()
	if !re.MatchString(a) || !re.MatchString(b) || a == b {
		t.Fatalf("%q %q", a, b)
	}
}

func TestCheckCanary(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	hit := write("hit.jsonl", `{"type":"attachment","content":"[heavyq · brief]\ncanary: hq-1a2b3c4d"}`+"\n")
	miss := write("miss.jsonl", `{"type":"user","content":"hello"}`+"\n")
	// The token straddles the 1 MiB read boundary: the overlap must find it.
	split := write("split.jsonl", strings.Repeat("x", 1<<20-5)+"hq-1a2b3c4d"+strings.Repeat("y", 100))
	notJSONL := write("t.txt", "hq-1a2b3c4d")

	cases := []struct {
		name, path, token string
		want              CanaryVerdict
	}{
		{"delivered", hit, "hq-1a2b3c4d", CanaryDelivered},
		{"missing", miss, "hq-1a2b3c4d", CanaryMissing},
		{"across the chunk boundary", split, "hq-1a2b3c4d", CanaryDelivered},
		{"empty token", hit, "", CanaryUnreadable},
		{"empty path", "", "hq-1a2b3c4d", CanaryUnreadable},
		{"relative path", "t.jsonl", "hq-1a2b3c4d", CanaryUnreadable},
		{"not a transcript file name", notJSONL, "hq-1a2b3c4d", CanaryUnreadable},
		{"no such file", filepath.Join(dir, "gone.jsonl"), "hq-1a2b3c4d", CanaryUnreadable},
		{"a directory", dir, "hq-1a2b3c4d", CanaryUnreadable},
	}
	for _, c := range cases {
		got, err := CheckCanary(c.path, c.token)
		if got != c.want {
			t.Errorf("%s: verdict %d (err %v), want %d", c.name, got, err, c.want)
		}
		if got == CanaryUnreadable && err == nil {
			t.Errorf("%s: unreadable must say why", c.name)
		}
	}
	if !strings.Contains(CanaryText(CanaryMissing, "hq-x", nil), "drops additionalContext") {
		t.Error("a missing canary must say what it may mean")
	}
	if !strings.Contains(CanaryText(CanaryDelivered, "hq-x", nil), "ok") {
		t.Error("delivered text")
	}
	if !strings.Contains(CanaryText(CanaryUnreadable, "hq-x", os.ErrNotExist), "not checked") {
		t.Error("unreadable text")
	}
}

func TestScanForFindsTokensAtEveryOffsetAroundTheBoundary(t *testing.T) {
	needle := []byte("hq-deadbeef")
	for off := 1<<20 - len(needle) - 2; off < 1<<20+3; off++ {
		buf := bytes.Repeat([]byte("."), 1<<20+64)
		copy(buf[off:], needle)
		ok, err := scanFor(bytes.NewReader(buf), needle)
		if err != nil || !ok {
			t.Fatalf("offset %d: %v %v", off, ok, err)
		}
	}
}
