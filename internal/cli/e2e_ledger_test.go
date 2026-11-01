package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// gitOutput runs git in dir with the rig's environment and returns stdout.
func gitOutput(t *testing.T, h *rig, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = h.env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// lastRecord is the newest ledger line.
func (h *rig) lastRecord() history.Record {
	h.t.Helper()
	recs := h.history()
	if len(recs) == 0 {
		h.t.Fatal("no ledger records")
	}
	return recs[len(recs)-1]
}

func TestE2ELedgerRecordsCommitTreeAndTags(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")

	if rc, out := h.run("4", "--session", "sess-1", "--item", "T-7", "--gate", "ready", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Version() != history.SchemaVersion || len(r.ID) != 26 || len(r.Repo) != 16 {
		t.Fatalf("v/id/repo: %+v", r)
	}
	if r.Worktree != repo || r.Subdir != "" || r.Commit != head || r.Tree != tree || r.Dirty {
		t.Fatalf("clean run identity: %+v\nwant worktree %s commit %s tree %s", r, repo, head, tree)
	}
	if r.Session != "sess-1" || r.Item != "T-7" || r.Gate != "ready" {
		t.Fatalf("tags: %+v", r)
	}

	// From a subdirectory, the repo and tree are the same; the subdir is recorded.
	if err := os.MkdirAll(filepath.Join(repo, "pkg", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.cwd = filepath.Join(repo, "pkg", "x")
	h.run("4", "--", "true")
	if s := h.lastRecord(); s.Subdir != "pkg/x" || s.Repo != r.Repo || s.Tree != tree {
		t.Fatalf("subdir run: %+v", s)
	}
	h.cwd = repo
}

func TestE2ELedgerDirtyTreeIdentityFollowsTheEdit(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")
	marker := filepath.Join(repo, "marker.txt")
	runTree := func() history.Record {
		t.Helper()
		if rc, out := h.run("4", "--", "true"); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		return h.lastRecord()
	}
	edit := func(body string) {
		t.Helper()
		if err := os.WriteFile(marker, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	edit("first edit\n")
	one := runTree()
	if !one.Dirty || !strings.HasPrefix(one.Tree, tree+"+") {
		t.Fatalf("a dirty run must record dirty and a tree extending HEAD's: %+v", one)
	}
	if again := runTree(); again.Tree != one.Tree {
		t.Fatalf("same edit, different tree: %s vs %s", again.Tree, one.Tree)
	}
	edit("second edit\n")
	if two := runTree(); !two.Dirty || two.Tree == one.Tree {
		t.Fatalf("the tree id must change with the edit: %+v", two)
	}
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if three := runTree(); !three.Dirty || three.Tree == one.Tree {
		t.Fatalf("an untracked file must change the tree id: %+v", three)
	}
	gitIn(t, h, repo, "checkout", "marker.txt")
	if err := os.Remove(filepath.Join(repo, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	if back := runTree(); back.Dirty || back.Tree != tree {
		t.Fatalf("reverting must restore the clean tree: %+v", back)
	}
}

// A command that edits the work tree it runs in proves nothing about either
// tree, so the ledger keeps the commit but no tree.
func TestE2ELedgerTreeChangedDuringRunIsNotRecorded(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	if rc, out := h.run("4", "--", "sh", "-c", "echo made-by-the-run > generated.txt"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Commit != head || r.Tree != "" || !r.Dirty {
		t.Fatalf("want commit %s, no tree, dirty: %+v", head, r)
	}
}

func TestE2ELedgerCacheHitRecordsTheProvenTree(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")
	argv := cachedCmd(h, "led", 0)
	h.run(cacheArgs([]string{"--cache", "--session", "s9"}, argv)...)
	h.run(cacheArgs([]string{"--cache", "--session", "s9"}, argv)...)
	recs := h.history()
	if len(recs) != 2 || recs[0].Cache != "miss" || recs[1].Cache != "hit" {
		t.Fatalf("history: %+v", recs)
	}
	for _, r := range recs {
		if r.Tree != tree || r.Dirty || r.Session != "s9" || r.Commit == "" {
			t.Fatalf("%s record: %+v", r.Cache, r)
		}
	}
}

func TestE2ELedgerRemoteRunRecordsTheShippedCommit(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	repo := gitRepo(t, h)
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	tree := gitOutput(t, h, repo, "rev-parse", "HEAD^{tree}")
	// An uncommitted edit does not travel; the record must not claim it did.
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("3", "--remote", "box", "--session", "r1", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Machine != "box" || r.Commit != head || r.Tree != tree || r.Dirty || r.Session != "r1" {
		t.Fatalf("remote record: %+v\nwant commit %s tree %s clean", r, head, tree)
	}
	// An older commit named with --ref is what ships, and what is recorded.
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "commit", "-q", "-am", "second")
	if rc, out := h.run("3", "--remote", "box", "--ref", head, "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Commit != head || r.Tree != tree || r.Dirty {
		t.Fatalf("--ref record: %+v", r)
	}
}

func TestE2ELedgerGaveUpRunStillCarriesIdentity(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	if rc, _ := h.run("pause", "local"); rc != 0 {
		t.Fatal("pause failed")
	}
	if rc, out := h.run("3", "--max-wait", "300ms", "--session", "g1", "--item", "I-1", "--", "true"); rc != ExitMaxWait {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Outcome() != history.OutcomeGaveUp || r.Commit != head || r.Session != "g1" || r.Item != "I-1" || r.Version() != 2 {
		t.Fatalf("gave-up record: %+v", r)
	}
}

func TestE2ELedgerOutsideGitHasNoCodeIdentity(t *testing.T) {
	h := newRig(t, baseConfig)
	if rc, out := h.run("4", "--session", "x", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	r := h.lastRecord()
	if r.Repo != "" || r.Worktree != "" || r.Commit != "" || r.Tree != "" || r.Dirty || r.Session != "x" || len(r.ID) != 26 {
		t.Fatalf("outside a repo: %+v", r)
	}
}

func TestE2ETagsFromEnvironmentAndValidation(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_SESSION=from-env", "HEAVYQ_ITEM=ENV-1")
	if rc, out := h.run("4", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Session != "from-env" || r.Item != "ENV-1" || r.Gate != "" {
		t.Fatalf("env tags: %+v", r)
	}
	// A flag wins over the environment.
	h.run("4", "--session", "from-flag", "--", "true")
	if r := h.lastRecord(); r.Session != "from-flag" || r.Item != "ENV-1" {
		t.Fatalf("flag must override env: %+v", r)
	}
	before := len(h.history())
	for _, bad := range [][]string{
		{"--session", "has space"}, {"--session", ""}, {"--item", "a/b"}, {"--gate", "x;y"},
		{"--session=" + strings.Repeat("a", 81)}, {"--item", "$(id)"}, {"--gate"},
	} {
		args := append(append([]string{"4"}, bad...), "--", "true")
		if rc, out := h.run(args...); rc != ExitUsage || !strings.Contains(out, "heavyq:") {
			t.Errorf("%v: rc=%d, want usage error\n%s", bad, rc, out)
		}
	}
	// A malformed environment value is refused, naming the variable.
	h.env = append(h.env, "HEAVYQ_ITEM=bad item")
	if rc, out := h.run("4", "--", "true"); rc != ExitUsage || !strings.Contains(out, "HEAVYQ_ITEM") {
		t.Fatalf("bad env item: rc=%d\n%s", rc, out)
	}
	if got := len(h.history()); got != before {
		t.Fatalf("refused runs must not write the ledger: %d lines, was %d", got, before)
	}
}

func TestE2EHistoryShowsCommitAndTags(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	head := gitOutput(t, h, repo, "rev-parse", "HEAD")
	h.run("4", "--session", "hs-1", "--gate", "ready", "--", "true")
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte("edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run("4", "--", "true")
	_, plain := h.run("history")
	if !strings.Contains(plain, "COMMIT") || !strings.Contains(plain, head[:8]+" ") || !strings.Contains(plain, head[:8]+"+") {
		t.Fatalf("history must show the short commit, + when dirty:\n%s", plain)
	}
	_, verbose := h.run("history", "-v")
	if !strings.Contains(verbose, "session hs-1") || !strings.Contains(verbose, "gate ready") || !strings.Contains(verbose, "tree ") {
		t.Fatalf("history -v must show tags and tree:\n%s", verbose)
	}
	if strings.Contains(plain, "session hs-1") {
		t.Fatalf("plain history stays one line per run:\n%s", plain)
	}
}
