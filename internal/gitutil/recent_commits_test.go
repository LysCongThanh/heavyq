package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestRecentCommits(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if got, err := RecentCommits(dir, time.Time{}, 10); err != nil || got != nil {
		t.Fatalf("no commits: got %v, %v", got, err)
	}
	commitFile(t, dir, "a.go", "1", "first")
	commitFile(t, dir, "b é.go", "2", "second")
	got, err := RecentCommits(dir, time.Time{}, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[0].Subject != "second" || len(got[0].Files) != 1 || got[0].Files[0] != "b é.go" || got[1].Files[0] != "a.go" {
		t.Errorf("newest first with files: %+v", got)
	}
	if got, _ := RecentCommits(dir, time.Time{}, 1); len(got) != 1 {
		t.Errorf("max 1: %d commits", len(got))
	}
	if got, _ := RecentCommits(dir, time.Now().Add(time.Hour), 10); len(got) != 0 {
		t.Errorf("since in the future: %d commits", len(got))
	}
}
