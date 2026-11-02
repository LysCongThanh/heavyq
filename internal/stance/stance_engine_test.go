package stance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
)

// rig is a real git repository, a user stance directory and a ledger, all in
// temporary directories, and a clock the test moves.
type rig struct {
	t                                         *testing.T
	repo, userDir, repoDir, ledger, trustPath string
	env                                       Env
	now                                       time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{t: t, repo: filepath.Join(root, "repo"), userDir: filepath.Join(root, "user", "stances"),
		ledger: filepath.Join(root, "state", "history.jsonl"), now: t0}
	r.repoDir = filepath.Join(r.repo, ".heavyq", "stances")
	r.trustPath = filepath.Join(root, "state", "stance-trust.json")
	if err := os.MkdirAll(r.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("init", "-q")
	r.env = Env{Dirs: Dirs{Repo: r.repoDir, User: r.userDir}, Trust: Trust{Path: r.trustPath}, Ledger: r.ledger, Now: func() time.Time { return r.now }}
	return r
}

func (r *rig) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r *rig) write(dir, name, content string) {
	r.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) commit(file, content, msg string) {
	r.t.Helper()
	r.write(r.repo, file, content)
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// record appends a run of lane "wt" to the ledger (and its index).
func (r *rig) record(min int, cmd string, exit int, tree string) {
	r.t.Helper()
	rec := run(min, cmd, exit, tree)
	if err := ledgerindex.Append(r.ledger, rec); err != nil {
		r.t.Fatal(err)
	}
}

const vietnameseStance = "+++\ndescription = \"Tư duy đột phá\"\ncooldown_minutes = 60\n[enter]\nkeywords = [\"đột phá\"]\n[exit]\ndecision_record = true\n+++\n<!-- core -->\n- Hãy lùi lại và nghĩ từ gốc vấn đề.\n<!-- /core -->\n"

func TestOnPromptKeywordEntersOnceAndRecordsState(t *testing.T) {
	r := newRig(t)
	r.write(r.userDir, "dot-pha.md", vietnameseStance)

	if e, err := r.env.OnPrompt(r.repo, "wt", "s1", "fix the login test"); e != nil || err != nil {
		t.Fatalf("an ordinary prompt: %v %v", e, err)
	}
	e, err := r.env.OnPrompt(r.repo, "wt", "s1", "Cần một hướng ĐỘT PHÁ cho lỗi này")
	if err != nil || e == nil || e.Stance != "dot-pha" || e.Trigger != TriggerKeyword {
		t.Fatalf("keyword prompt: %+v %v", e, err)
	}
	if !strings.Contains(e.Context, "Hãy lùi lại và nghĩ từ gốc vấn đề.") || !strings.Contains(e.Context, `"đột phá"`) {
		t.Errorf("context lacks the stance core or the keyword:\n%s", e.Context)
	}
	// Announced once: the same prompt again says nothing.
	if e, err := r.env.OnPrompt(r.repo, "wt", "s1", "đột phá"); e != nil || err != nil {
		t.Errorf("second prompt: %v %v", e, err)
	}
	// Another lane has its own state.
	if e, _ := r.env.OnPrompt(r.repo, "other", "s2", "đột phá"); e == nil {
		t.Error("another lane did not enter")
	}
	store, _ := OpenStore(mustCommon(t, r))
	st, _ := store.Load("wt")
	if st.Active == nil || st.Active.Stance != "dot-pha" || st.Active.Session != "s1" {
		t.Fatalf("lane state: %+v", st.Active)
	}
	if evs, _ := store.Events(); len(evs) != 2 || evs[0].Op != OpEnter || evs[0].Trigger != TriggerKeyword {
		t.Errorf("events: %+v", evs)
	}
}

func mustCommon(t *testing.T, r *rig) string {
	t.Helper()
	set, err := r.env.LoadSet(r.repo)
	if err != nil || set.Common == "" {
		t.Fatalf("common dir: %q %v", set.Common, err)
	}
	return set.Common
}

func TestOnPromptEvidenceFailureClassAndChurn(t *testing.T) {
	r := newRig(t)
	// No evidence: a quiet lane.
	if e, _ := r.env.OnPrompt(r.repo, "wt", "s", "carry on"); e != nil {
		t.Fatalf("entered without evidence: %+v", e)
	}
	// The same failure on three trees, from the real ledger index.
	r.record(1, "go test ./...", 1, "a")
	r.record(2, "go test ./...", 1, "b")
	if e, _ := r.env.OnPrompt(r.repo, "wt", "s", "carry on"); e != nil {
		t.Fatalf("two trees are not enough: %+v", e)
	}
	r.record(3, "go test ./...", 1, "c")
	e, err := r.env.OnPrompt(r.repo, "wt", "s", "carry on")
	if err != nil || e == nil || e.Stance != "breakthrough" || e.Trigger != TriggerFailureClass ||
		!strings.Contains(e.Why, "same failure class on 3 trees: go test ./... exit 1") {
		t.Fatalf("failure class: %+v %v", e, err)
	}
	if !strings.Contains(e.Context, "why now: same failure class on 3 trees") {
		t.Errorf("injection:\n%s", e.Context)
	}

	// Churn from real git history: a revert and a redo of the same files, no proof since.
	r2 := newRig(t)
	r2.now = time.Now() // git stamps real time; the churn window is the last 24 hours
	r2.commit("a.go", "1", "add a")
	r2.commit("a.go", "2", "again")
	if e, _ := r2.env.OnPrompt(r2.repo, "wt", "s", "carry on"); e != nil {
		t.Fatalf("one rewrite is below the threshold of 2: %+v", e)
	}
	r2.git("revert", "--no-edit", "HEAD")
	e, err = r2.env.OnPrompt(r2.repo, "wt", "s", "carry on")
	if err != nil || e == nil || e.Trigger != TriggerChurn || !strings.Contains(e.Why, "1 reverts and 1 rewrites of the same files without a proof since: a.go") {
		t.Fatalf("churn: %+v %v", e, err)
	}
}

func TestChurnWindowStartsAtTheLastProof(t *testing.T) {
	r := newRig(t)
	r.commit("a.go", "1", "add a")
	r.commit("a.go", "2", "again")
	r.git("revert", "--no-edit", "HEAD")
	r.now = time.Now()
	set, _ := r.env.LoadSet(r.repo)
	ev, err := r.env.CollectEvidence(r.repo, "wt", set.Injectable)
	if err != nil || ev.Churn.Count() != 2 {
		t.Fatalf("before a proof: %+v %v", ev.Churn, err)
	}
	// A passing run after those commits ends the window.
	rec := history.Record{End: r.now.Add(time.Minute), Lane: "wt", Repo: "r", Cmd: "go test", Tree: "t", Exit: 0}
	if err := ledgerindex.Append(r.ledger, rec); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(2 * time.Minute)
	if ev, _ = r.env.CollectEvidence(r.repo, "wt", set.Injectable); ev.Churn.Count() != 0 {
		t.Errorf("after a proof: %+v", ev.Churn)
	}
}

func TestOnPromptCooldownMuteAndTimebox(t *testing.T) {
	r := newRig(t)
	r.write(r.userDir, "dot-pha.md", vietnameseStance)
	enter := func() *Entry {
		e, err := r.env.OnPrompt(r.repo, "wt", "s1", "đột phá")
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if enter() == nil {
		t.Fatal("first keyword did not enter")
	}
	if _, err := r.env.Leave(r.repo, "wt", "", true); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(10 * time.Minute)
	if enter() != nil {
		t.Error("entered again inside the 60 minute cooldown")
	}
	r.now = r.now.Add(61 * time.Minute)
	if enter() == nil {
		t.Fatal("did not enter after the cooldown")
	}
	if err := r.env.Off(r.repo, "wt", "s1"); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(10 * time.Hour)
	if enter() != nil {
		t.Error("a muted lane entered")
	}
	if _, ok := r.env.ActiveFor(r.repo, "wt"); ok {
		t.Error("ActiveFor on a muted lane")
	}
}

func TestActiveForReinjectsTheSameCompactRender(t *testing.T) {
	r := newRig(t)
	r.write(r.userDir, "dot-pha.md", strings.Replace(vietnameseStance, "[exit]\ndecision_record = true\n", "[exit]\ntimebox_minutes = 30\n", 1))
	if _, ok := r.env.ActiveFor(r.repo, "wt"); ok {
		t.Fatal("ActiveFor before any entry")
	}
	e, _ := r.env.OnPrompt(r.repo, "wt", "s1", "đột phá")
	got, ok := r.env.ActiveFor(r.repo, "wt")
	if !ok || got != e.Context {
		t.Fatalf("ActiveFor = %q %v, want the entry render %q", got, ok, e.Context)
	}
	if _, ok := r.env.ActiveFor(r.repo, "other"); ok {
		t.Error("ActiveFor of a lane that never entered")
	}
	r.now = r.now.Add(31 * time.Minute) // past the time box: the stance is over
	if _, ok := r.env.ActiveFor(r.repo, "wt"); ok {
		t.Error("ActiveFor past the time box")
	}
	if _, ok := r.env.ActiveFor(t.TempDir(), "wt"); ok {
		t.Error("ActiveFor outside a repository")
	}
}

func TestLeaveNeedsTheDecisionRecord(t *testing.T) {
	r := newRig(t)
	if _, err := r.env.EnterManual(r.repo, "wt", "s", "item-1", "breakthrough", ""); err != nil {
		t.Fatal(err)
	}
	res, err := r.env.Leave(r.repo, "wt", "", false)
	if err != nil || res.Left || len(res.Check.Problems) == 0 || !strings.HasSuffix(res.Record, filepath.Join(".heavyq", "decisions", "item-1.md")) {
		t.Fatalf("without a record: %+v %v", res, err)
	}
	st, _ := r.env.Status(r.repo, "wt")
	if st.Active == nil || st.Record == nil || st.Record.Exists {
		t.Fatalf("status: %+v", st)
	}
	r.write(filepath.Join(r.repo, ".heavyq", "decisions"), "item-1.md", "# only a title\n")
	if res, _ = r.env.Leave(r.repo, "wt", "", false); res.Left || len(res.Check.Problems) < 4 {
		t.Fatalf("incomplete record: %+v", res)
	}
	r.write(filepath.Join(r.repo, ".heavyq", "decisions"), "item-1.md", validRecord)
	if res, err = r.env.Leave(r.repo, "wt", "", false); err != nil || !res.Left || res.Reason != ExitDecisionRecord {
		t.Fatalf("complete record: %+v %v", res, err)
	}
	if _, err := r.env.Leave(r.repo, "wt", "", false); err == nil {
		t.Error("leaving a lane that is in no stance must be an error")
	}
	// Forcing is logged as such.
	if _, err := r.env.EnterManual(r.repo, "wt", "s", "item-2", "breakthrough", ""); err != nil {
		t.Fatal(err)
	}
	if res, _ = r.env.Leave(r.repo, "wt", "", true); !res.Left || res.Reason != ExitForced {
		t.Fatalf("forced: %+v", res)
	}
	// The stance, its decision and its measurement are in the events log.
	store, _ := OpenStore(mustCommon(t, r))
	evs, _ := store.Events()
	var reasons []string
	for _, e := range evs {
		if e.Op == OpExit {
			reasons = append(reasons, e.Reason)
		}
	}
	if strings.Join(reasons, ",") != "decision_record,forced" {
		t.Errorf("exit reasons = %v", reasons)
	}
}
