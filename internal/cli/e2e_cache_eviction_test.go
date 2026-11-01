package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2ECacheTTLAndSizeEviction(t *testing.T) {
	h := newRig(t, baseConfig+"[cache]\nttl = \"3s\"\nmax_size_mb = 1\nmax_entry_mb = 1\n")
	gitRepo(t, h)
	argv := cachedCmd(h, "t", 0)
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("t") != 1 {
		t.Fatal("expected a hit within the TTL")
	}
	time.Sleep(3100 * time.Millisecond)
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("t") != 2 {
		t.Fatalf("expired entry must re-run, runs=%d", h.runs("t"))
	}

	h.setConfig(baseConfig + "[cache]\nmax_size_mb = 1\nmax_entry_mb = 1\n")
	big := func(tag string) []string { // ~400 KB of output, counted
		return []string{"sh", "-c", fmt.Sprintf(`echo run >> %q; head -c 400000 /dev/zero | tr '\0' %s`,
			filepath.Join(h.root, "count-"+tag), tag)}
	}
	for _, tag := range []string{"x", "y", "z"} {
		if rc, _ := h.run(cacheArgs([]string{"--cache"}, big(tag))...); rc != 0 {
			t.Fatal(tag)
		}
	}
	_, out := h.run("cache", "ls", "--json")
	var metas []struct{ Cmd string }
	if err := json.Unmarshal([]byte(out), &metas); err != nil || len(metas) != 2 {
		t.Fatalf("want 2 entries under the 1 MiB cap: %v\n%s", err, out)
	}
	h.run(cacheArgs([]string{"--cache"}, big("x"))...)
	h.run(cacheArgs([]string{"--cache"}, big("z"))...)
	if h.runs("x") != 2 || h.runs("z") != 1 {
		t.Fatalf("oldest entry must be evicted first: x=%d z=%d", h.runs("x"), h.runs("z"))
	}
	if rc, out := h.run("cache", "clear"); rc != 0 || !strings.Contains(out, "removed") {
		t.Fatal(out)
	}
	if _, out := h.run("cache", "ls"); !strings.Contains(out, "no cached results") {
		t.Fatal(out)
	}
}
