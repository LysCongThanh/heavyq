package cli

import (
	"reflect"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

func TestArgvWithoutSiblingPathsHidesOnlyDeclaredSiblings(t *testing.T) {
	sibs := []gitutil.Sibling{{Name: "core-lib"}, {Name: "schema"}}
	const root = "/src/app"
	for _, c := range []struct {
		argv    []string
		outside bool
	}{
		{[]string{"make", "-C", "../core-lib", "test"}, false},
		{[]string{"sh", "-c", "cat ../schema/a.json ../core-lib/b"}, false},
		{[]string{"sh", "-c", "cd ../core-lib && ls"}, false},
		{[]string{"cat", "../other/a"}, true},
		{[]string{"cat", "../core-library/a"}, true}, // a longer name is another directory
		{[]string{"cat", "../../core-lib/a"}, true},  // two levels up is not the sibling
		{[]string{"sh", "-c", "cat ../core-lib/a ../other/b"}, true},
		{[]string{"cat", "~/x"}, true},
	} {
		got := schedule.OutsideRepoReference(argvWithoutSiblingPaths(c.argv, sibs), root) != ""
		if got != c.outside {
			t.Errorf("%q: outside = %v, want %v", c.argv, got, c.outside)
		}
	}
	argv := []string{"cat", "../core-lib/a"}
	if got := argvWithoutSiblingPaths(argv, nil); !reflect.DeepEqual(got, argv) {
		t.Errorf("without siblings the argv changed: %q", got)
	}
	if argv[1] != "../core-lib/a" {
		t.Error("the caller's argv was modified")
	}
}

func TestGateRunArgsCarryWithAndNeedGH(t *testing.T) {
	g := repoconfig.Gate{Name: "ready", Prio: 3, Remote: "auto", With: []string{"core-lib@v1", "schema"}, NeedGH: true}
	got := gateRunArgs(g, "make test", "", false)
	want := []string{"3", "--gate", "ready", "--remote", "auto", "--strict", "--cache", "--with", "core-lib@v1", "--with", "schema", "--need-gh", "--"}
	want = append(want, proof.Argv("make test")...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q\nwant   %q", got, want)
	}
}
