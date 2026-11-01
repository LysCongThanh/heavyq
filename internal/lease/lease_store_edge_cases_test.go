package lease

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerValidation(t *testing.T) {
	s := newStore(t)
	good := owner(t, "laneA", "s1")
	bad := []Owner{
		{},
		{Lane: "l", Pid: 1},
		{Lane: "l", Start: "x"},
		{Lane: "l", Pid: -1, Start: "x"},
		{Lane: "l\nx", Pid: 1, Start: "x"},
		{Lane: string(make([]byte, 300)), Pid: 1, Start: "x"},
	}
	for _, o := range bad {
		if _, err := s.Claim("x", o); err == nil {
			t.Errorf("owner %+v accepted", o)
		}
	}
	if _, _, err := s.ClaimNext([]string{"ok", "../bad"}, good); err == nil {
		t.Error("invalid candidate accepted")
	}
	if ls, _ := s.List(); len(ls) != 0 {
		t.Errorf("invalid ClaimNext left leases: %+v", ls)
	}
}

func TestCorruptLeaseIsReclaimed(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.path("x"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ls, err := s.List(); err != nil || len(ls) != 0 {
		t.Fatalf("list = %+v %v", ls, err)
	}
	if _, err := s.Claim("x", owner(t, "laneA", "s1")); err != nil {
		t.Fatal(err)
	}
	if evs := wantOps(t, s, "reclaim"); evs[0].Reason != "corrupt" {
		t.Fatalf("event = %+v", evs[0])
	}
}

func TestSlashItemsAndPermissions(t *testing.T) {
	s := newStore(t)
	a := owner(t, "laneA", "s1")
	if _, err := s.Claim("plans/foo/phase-01", a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim("plans+foo+phase-01", a); err == nil {
		t.Fatal("'+' must not be a valid id character")
	}
	ls, _ := s.List()
	if len(ls) != 1 || ls[0].Item != "plans/foo/phase-01" {
		t.Fatalf("list = %+v", ls)
	}
	for path, want := range map[string]os.FileMode{
		s.dir: 0o700, s.path("plans/foo/phase-01"): 0o600, s.eventsPath: 0o600, s.lockPath: 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v err %v, want %v", filepath.Base(path), fi.Mode().Perm(), err, want)
		}
	}
}

func TestOpenRejectsMissingCommonDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Error("empty accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("missing dir accepted")
	}
}
