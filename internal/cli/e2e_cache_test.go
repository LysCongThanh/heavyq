package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cachedCmd is an argv that counts its executions in a file outside the
// repo (so the work tree stays clean), prints to both streams and exits rc.
func cachedCmd(h *rig, tag string, rc int) []string {
	script := fmt.Sprintf(`echo run >> %q; echo out1-%s; sleep 0.05; echo err1-%s >&2; sleep 0.05; echo out2-%s; exit %d`,
		filepath.Join(h.root, "count-"+tag), tag, tag, tag, rc)
	return []string{"sh", "-c", script}
}

func (h *rig) runs(tag string) int { return strings.Count(h.read("count-"+tag), "run\n") }

func cacheArgs(extra []string, argv []string) []string {
	return append(append(append([]string{"4"}, extra...), "--"), argv...)
}

// commandOutput strips heavyq's own lines, leaving what the command printed.
func commandOutput(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "heavyq: ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func TestE2ECacheHitReplaysOutputAndExitCode(t *testing.T) {
	h := newRig(t, baseConfig+"[cache]\ncache_failures = true\n")
	gitRepo(t, h)
	argv := cachedCmd(h, "a", 3)
	rc1, out1 := h.run(cacheArgs([]string{"--cache"}, argv)...)
	rc2, out2 := h.run(cacheArgs([]string{"--cache"}, argv)...)
	if rc1 != 3 || rc2 != 3 || h.runs("a") != 1 {
		t.Fatalf("rc %d/%d, runs %d\n%s\n---\n%s", rc1, rc2, h.runs("a"), out1, out2)
	}
	if want := "out1-a\nerr1-a\nout2-a\n"; commandOutput(out1) != want || commandOutput(out2) != want {
		t.Fatalf("replay differs:\n%q\n%q", commandOutput(out1), commandOutput(out2))
	}
	if !strings.Contains(out2, "heavyq: cached result from ") || !strings.Contains(out2, "(local)") {
		t.Fatalf("missing cache note:\n%s", out2)
	}
	// Streams are replayed to the right file descriptors.
	_, stdout, stderr := h.runStdin("", cacheArgs([]string{"--cache"}, argv)...)
	if stdout != "out1-a\nout2-a\n" || !strings.HasPrefix(stderr, "err1-a\n") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	hist := h.read("history.jsonl")
	if strings.Count(hist, `"cache":"miss"`) != 1 || strings.Count(hist, `"cache":"hit"`) != 2 {
		t.Fatalf("history:\n%s", hist)
	}
	// A different argv, env allowlist value or commit is a different key.
	if rc, _ := h.run(cacheArgs([]string{"--cache"}, cachedCmd(h, "b", 0))...); rc != 0 || h.runs("b") != 1 {
		t.Fatal("other command must run")
	}
	if rc, _ := h.run(cacheArgs(nil, argv)...); rc != 3 || h.runs("a") != 2 {
		t.Fatal("without --cache the command must run")
	}
}

func TestE2ECacheSkipsDirtyTreesButNotIgnoredFiles(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	argv := cachedCmd(h, "d", 0)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("marker.txt", "edited\n") // tracked change
	_, out := h.run(cacheArgs([]string{"--cache", "-v"}, argv)...)
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("d") != 2 || !strings.Contains(out, "cache not used: work tree has uncommitted or untracked files") {
		t.Fatalf("dirty tracked file: runs=%d\n%s", h.runs("d"), out)
	}
	gitIn(t, h, repo, "checkout", "marker.txt")
	write("new.txt", "x") // untracked, not ignored
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("d") != 3 || !strings.Contains(h.read("history.jsonl"), `"cache":"skip"`) {
		t.Fatalf("untracked file must disable the cache, runs=%d", h.runs("d"))
	}
	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	write(".gitignore", "*.log\n")
	gitIn(t, h, repo, "add", ".gitignore")
	gitIn(t, h, repo, "commit", "-q", "-m", "ignore logs")
	write("debug.log", "ignored")
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("d") != 4 {
		t.Fatalf("ignored files must not prevent caching, runs=%d", h.runs("d"))
	}
	gitIn(t, h, repo, "commit", "-q", "--allow-empty", "-m", "same tree") // same tree: still a hit
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	write("marker.txt", "v2\n")
	gitIn(t, h, repo, "commit", "-q", "-am", "new tree")
	h.run(cacheArgs([]string{"--cache"}, argv)...)
	if h.runs("d") != 5 {
		t.Fatalf("key must follow the tree of HEAD, runs=%d", h.runs("d"))
	}
}

func TestE2ECacheFailuresNotCachedByDefault(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h)
	argv := cachedCmd(h, "f", 1)
	for i := 0; i < 2; i++ {
		if rc, _ := h.run(cacheArgs([]string{"--cache"}, argv)...); rc != 1 {
			t.Fatalf("rc %d", rc)
		}
	}
	if h.runs("f") != 2 || !strings.Contains(h.read("history.jsonl"), "cache_failures = false") {
		t.Fatalf("failures must re-run by default, runs=%d", h.runs("f"))
	}
}

func TestE2ECoalesceFiveIdenticalRunsExecuteOnce(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h)
	script := fmt.Sprintf(`echo run >> %q; %s; echo shared-result; exit 0`, filepath.Join(h.root, "count-p"), h.gateFile("go"))
	var ps []*proc
	for i := 1; i <= 5; i++ {
		ps = append(ps, h.start("4", "--cache", "--lane", fmt.Sprintf("agent%d", i), "--", "sh", "-c", script))
	}
	var status string
	h.until("4 runs waiting on the leader", func() bool {
		_, status = h.run("status")
		return strings.Contains(status, "4 waiting on lane agent")
	})
	if len(h.snapshot().Running) != 1 || len(h.snapshot().Waiting) != 0 {
		t.Fatalf("followers must not queue tickets:\n%s", status)
	}
	h.touch("go")
	for i, p := range ps {
		if rc := p.wait(t, 30*time.Second); rc != 0 || !strings.Contains(p.out.String(), "shared-result") {
			t.Fatalf("run %d: rc=%d\n%s", i+1, rc, p.out)
		}
	}
	if h.runs("p") != 1 {
		t.Fatalf("command ran %d times", h.runs("p"))
	}
	hist := h.read("history.jsonl")
	if strings.Count(hist, `"cache":"miss"`) != 1 || strings.Count(hist, `"cache":"coalesced"`) != 4 {
		t.Fatalf("history:\n%s", hist)
	}
	_, out := h.run("cache", "stats", "--json")
	var st cacheStatsView
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.Coalesced != 4 || st.Misses != 1 || st.Entries != 1 {
		t.Fatalf("stats %+v (%v)\n%s", st, err, out)
	}
}

func TestE2ECoalescedFailureIsSharedButNotCached(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h)
	script := fmt.Sprintf(`echo run >> %q; %s; echo boom; exit 7`, filepath.Join(h.root, "count-x"), h.gateFile("go"))
	a := h.start("4", "--cache", "--lane", "a", "--", "sh", "-c", script)
	h.running(1)
	b := h.start("4", "--cache", "--lane", "b", "--", "sh", "-c", script)
	h.until("b waits on a", func() bool { _, s := h.run("status"); return strings.Contains(s, "1 waiting on lane a") })
	h.touch("go")
	if a.wait(t, 30*time.Second) != 7 || b.wait(t, 30*time.Second) != 7 || h.runs("x") != 1 {
		t.Fatalf("waiting run must reuse the failure: runs=%d\n%s", h.runs("x"), b.out)
	}
	if rc, _ := h.run("4", "--cache", "--", "sh", "-c", script); rc != 7 || h.runs("x") != 2 {
		t.Fatal("a later run must not get a cached failure")
	}
}
