package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// commitAt makes a commit with every file in files written, at a fixed time.
func commitAt(t *testing.T, repo, msg string, at time.Time, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "add", ".")
	stamp := at.UTC().Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", stamp)
	t.Setenv("GIT_COMMITTER_DATE", stamp)
	gitIn(t, repo, "commit", "-q", "-m", msg)
}

var base0 = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

// mergeMainInto merges main into the checked-out branch, resolving every
// conflict by writing resolved, at time at.
func mergeMainInto(t *testing.T, repo string, at time.Time, resolved map[string]string) {
	t.Helper()
	stamp := at.UTC().Format(time.RFC3339)
	t.Setenv("GIT_AUTHOR_DATE", stamp)
	t.Setenv("GIT_COMMITTER_DATE", stamp)
	if tryGit(repo, "merge", "--no-edit", "--no-ff", "main") == nil {
		return // merged cleanly
	}
	for name, body := range resolved {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "--no-edit")
}

// tryGit runs git in dir and reports its failure, for the merges that conflict.
func tryGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	return cmd.Run()
}

func TestBranchesAndMainMergesAndConflictFiles(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "r")
	mkRepo(t, repo) // main: f = "x"
	gitIn(t, repo, "checkout", "-q", "-b", "feat/login")
	commitAt(t, repo, "login", base0.Add(1*time.Minute), map[string]string{"CHANGELOG.md": "login\n", "g": "1"})
	gitIn(t, repo, "checkout", "-q", "main")
	commitAt(t, repo, "main moves", base0.Add(5*time.Minute), map[string]string{"CHANGELOG.md": "other\n"})
	commitAt(t, repo, "main moves again", base0.Add(6*time.Minute), map[string]string{"h": "2"})
	gitIn(t, repo, "checkout", "-q", "feat/login")
	mergeMainInto(t, repo, base0.Add(12*time.Minute), map[string]string{"CHANGELOG.md": "login\nother\n"}) // conflict
	commitAt(t, repo, "more login", base0.Add(14*time.Minute), map[string]string{"g": "2"})
	gitIn(t, repo, "checkout", "-q", "main")
	commitAt(t, repo, "main moves a third time", base0.Add(20*time.Minute), map[string]string{"k": "3"})
	gitIn(t, repo, "checkout", "-q", "feat/login")
	mergeMainInto(t, repo, base0.Add(22*time.Minute), nil) // clean

	// A branch merged into feat/login that is not main must not count.
	gitIn(t, repo, "checkout", "-q", "-b", "side", "main")
	commitAt(t, repo, "side", base0.Add(23*time.Minute), map[string]string{"s": "1"})
	gitIn(t, repo, "checkout", "-q", "feat/login")
	gitIn(t, repo, "merge", "-q", "--no-edit", "--no-ff", "side")

	bs, err := Branches(repo, base0, 10)
	if err != nil || len(bs) != 3 {
		t.Fatalf("branches: %+v, %v", bs, err)
	}
	if bs[0].Name != "feat/login" || bs[0].Ref != "refs/heads/feat/login" {
		t.Errorf("newest tip first: %+v", bs)
	}
	if got, _ := Branches(repo, base0.Add(time.Hour), 10); len(got) != 0 {
		t.Errorf("tips older than since are skipped: %+v", got)
	}

	merges, err := MainMerges(repo, "main", "feat/login", base0, base0.Add(time.Hour))
	if err != nil || len(merges) != 2 {
		t.Fatalf("main merges: %+v, %v", merges, err)
	}
	if !merges[0].At.Equal(base0.Add(12*time.Minute)) || !merges[0].PrevAt.Equal(base0.Add(1*time.Minute)) || !merges[1].At.Equal(base0.Add(22*time.Minute)) {
		t.Errorf("oldest first, with the previous commit's time: %+v", merges)
	}
	if got, _ := MainMerges(repo, "main", "feat/login", base0.Add(15*time.Minute), base0.Add(time.Hour)); len(got) != 1 {
		t.Errorf("the time bounds apply to the merge: %+v", got)
	}

	files, err := MergeConflictFiles(repo, merges[0])
	if err != nil || !reflect.DeepEqual(files, []string{"CHANGELOG.md"}) {
		t.Errorf("conflict files of the first merge: %v, %v", files, err)
	}
	if files, err := MergeConflictFiles(repo, merges[1]); err != nil || len(files) != 0 {
		t.Errorf("the second merge was clean: %v, %v", files, err)
	}
	if _, err := MergeConflictFiles(repo, MergeCommit{SHA: "--help", Parent1: "x", Parent2: "y"}); err == nil {
		t.Error("a ref that is not a sha must be refused")
	}
	if _, err := MainMerges(repo, "--output=/tmp/x", "feat/login", base0, base0); err == nil {
		t.Error("an option as a ref must be refused")
	}
}

// Taking one side whole leaves no trace in the merge commit's combined diff;
// re-merging the parents still finds the conflict.
func TestConflictResolvedByTakingOneSideIsStillFound(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "r")
	mkRepo(t, repo)
	gitIn(t, repo, "checkout", "-q", "-b", "feat")
	commitAt(t, repo, "feat", base0, map[string]string{"f": "mine\n"})
	gitIn(t, repo, "checkout", "-q", "main")
	commitAt(t, repo, "main", base0.Add(time.Minute), map[string]string{"f": "theirs\n"})
	gitIn(t, repo, "checkout", "-q", "feat")
	mergeMainInto(t, repo, base0.Add(5*time.Minute), map[string]string{"f": "mine\n"})
	merges, err := MainMerges(repo, "main", "feat", base0, base0.Add(time.Hour))
	if err != nil || len(merges) != 1 {
		t.Fatalf("%+v, %v", merges, err)
	}
	if files, err := MergeConflictFiles(repo, merges[0]); err != nil || !reflect.DeepEqual(files, []string{"f"}) {
		t.Errorf("files: %v, %v", files, err)
	}
	if files, err := combinedDiffFiles(repo, merges[0].SHA); err != nil || len(files) != 0 {
		t.Errorf("the old-git fallback cannot see it, by design: %v, %v", files, err)
	}
}

func TestParseMergeTreeConflicts(t *testing.T) {
	got := parseMergeTreeConflicts("0123abc\n" + "b.md\x00a.md\x00\x00")
	if !reflect.DeepEqual(got, []string{"a.md", "b.md"}) {
		t.Errorf("%v", got)
	}
	if got := parseMergeTreeConflicts("0123abc\x00"); len(got) != 0 {
		t.Errorf("%v", got)
	}
}
