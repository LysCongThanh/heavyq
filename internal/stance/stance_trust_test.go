package stance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoStance is a stance a cloned repository ships; it asks for the word "ship-it".
const repoStance = "+++\ndescription = \"from the repo\"\n[enter]\nkeywords = [\"ship-it\"]\n+++\n<!-- core -->\n- Repo text that reaches the agent only once trusted.\n<!-- /core -->\n"

func TestUntrustedRepoStancesAreListedButNeverInjected(t *testing.T) {
	r := newRig(t)
	r.write(r.repoDir, "shipit.md", repoStance)

	set, err := r.env.LoadSet(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	s, listed := Find(set.All, "shipit")
	if !listed || !set.Untrusted(s) || set.RepoTrusted || set.RepoHash == "" {
		t.Fatalf("an untrusted repo stance must be listed and flagged: %+v trusted=%v", s, set.RepoTrusted)
	}
	if _, ok := Find(set.Injectable, "shipit"); ok {
		t.Fatal("an untrusted repo stance is injectable")
	}
	if e, err := r.env.OnPrompt(r.repo, "wt", "s", "please ship-it"); e != nil || err != nil {
		t.Fatalf("an untrusted keyword entered: %v %v", e, err)
	}
	if _, err := r.env.EnterManual(r.repo, "wt", "s", "", "shipit", ""); err == nil || !strings.Contains(err.Error(), "heavyq trust") {
		t.Fatalf("manual enter of an untrusted stance: %v", err)
	}
	// Built-in and user stances need no trust.
	if e, _ := r.env.OnPrompt(r.repo, "wt", "s", "I want a breakthrough"); e == nil || e.Stance != "breakthrough" {
		t.Errorf("a built-in stance must work in an untrusted repo: %+v", e)
	}
}

func TestTrustIsAContentHashAndAnEditRevokesIt(t *testing.T) {
	r := newRig(t)
	r.write(r.repoDir, "shipit.md", repoStance)
	set, _ := r.env.LoadSet(r.repo)
	if err := r.env.Trust.Grant(set.Common, set.RepoHash, r.now); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(r.trustPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("trust file: %v %v", fi, err)
	}
	e, err := r.env.OnPrompt(r.repo, "wt", "s", "ship-it")
	if err != nil || e == nil || e.Stance != "shipit" || !strings.Contains(e.Context, "Repo text that reaches the agent only once trusted.") {
		t.Fatalf("trusted repo stance: %+v %v", e, err)
	}

	// An edit changes the hash: untrusted again, until `heavyq trust`.
	r.write(r.repoDir, "shipit.md", repoStance+"Now with a new instruction.\n")
	set2, _ := r.env.LoadSet(r.repo)
	if set2.RepoTrusted || set2.RepoHash == set.RepoHash {
		t.Fatal("an edited stance file stayed trusted")
	}
	if _, ok := r.env.ActiveFor(r.repo, "wt"); ok {
		t.Error("ActiveFor must not re-inject a stance whose trust was lost")
	}
	// So does adding or removing a file, and a rename.
	h1, _, _ := ContentHash(r.repoDir)
	r.write(r.repoDir, "extra.md", "+++\n+++\nx\n")
	h2, names, _ := ContentHash(r.repoDir)
	if h1 == h2 || len(names) != 2 {
		t.Errorf("a new file must change the hash: %s %s %v", h1, h2, names)
	}
	if err := os.Rename(filepath.Join(r.repoDir, "extra.md"), filepath.Join(r.repoDir, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	if h3, _, _ := ContentHash(r.repoDir); h3 == h2 {
		t.Error("a rename must change the hash")
	}
	// Dot files and other extensions are not stances and not hashed.
	h4, _, _ := ContentHash(r.repoDir)
	r.write(r.repoDir, ".hidden.md", "x")
	r.write(r.repoDir, "notes.txt", "x")
	if h5, _, _ := ContentHash(r.repoDir); h5 != h4 {
		t.Error("files the loader ignores must not change the hash")
	}

	set3, _ := r.env.LoadSet(r.repo)
	if err := r.env.Trust.Grant(set3.Common, set3.RepoHash, r.now); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.env.LoadSet(r.repo); !again.RepoTrusted {
		t.Error("re-trusting the new content did not work")
	}
	if was, err := r.env.Trust.Revoke(set3.Common); err != nil || !was {
		t.Errorf("revoke: %v %v", was, err)
	}
	if again, _ := r.env.LoadSet(r.repo); again.RepoTrusted {
		t.Error("a revoked repo is still trusted")
	}
}

func TestTrustFailsClosed(t *testing.T) {
	dir := t.TempDir()
	tr := Trust{Path: filepath.Join(dir, "trust.json")}
	if tr.Trusted("/repo", "sha256:x") {
		t.Error("nothing is trusted before a grant")
	}
	if tr.Trusted("/repo", "") {
		t.Error("the empty hash (no stance files) is never a trust")
	}
	if err := os.WriteFile(tr.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tr.Trusted("/repo", "sha256:x") {
		t.Error("a damaged trust file must trust nothing")
	}
	if _, err := tr.Entries(); err == nil {
		t.Error("a damaged trust file must be reported")
	}
	if err := tr.Grant("/repo", "sha256:x", t0); err != nil { // a grant starts over from a damaged file
		t.Fatal(err)
	}
	if !tr.Trusted("/repo", "sha256:x") || tr.Trusted("/repo", "sha256:y") || tr.Trusted("/other", "sha256:x") {
		t.Error("trust is per repository and per hash")
	}
}
